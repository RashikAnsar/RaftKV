package kvp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
	"go.uber.org/zap"
)

// ServerConfig holds KVP server configuration
type ServerConfig struct {
	Addr            string        // TCP address to listen on (e.g., ":6379")
	ReadTimeout     time.Duration // Read timeout for connections
	WriteTimeout    time.Duration // Write timeout for connections
	MaxConnections  int           // Maximum concurrent connections
	IdleTimeout     time.Duration // Idle connection timeout
	ShutdownTimeout time.Duration // Graceful shutdown timeout
}

// DefaultServerConfig returns default configuration
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Addr:            ":6379",
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		MaxConnections:  10000,
		IdleTimeout:     5 * time.Minute,
		ShutdownTimeout: 30 * time.Second,
	}
}

// Server is a KVP (Redis-compatible) server
type Server struct {
	config   ServerConfig
	store    storage.Store
	registry *CommandRegistry
	logger   *zap.Logger

	listener net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	shutdown chan struct{}
	closed   bool
}

// NewServer creates a new KVP server
func NewServer(config ServerConfig, store storage.Store, logger *zap.Logger) *Server {
	return &Server{
		config:   config,
		store:    store,
		registry: NewCommandRegistry(store),
		logger:   logger,
		conns:    make(map[net.Conn]struct{}),
		shutdown: make(chan struct{}),
	}
}

// Start starts the KVP server
func (s *Server) Start() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("server is closed")
	}
	s.mu.Unlock()

	listener, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.listener = listener
	s.logger.Info("KVP server started", zap.String("addr", s.config.Addr))

	// Accept connections
	go s.acceptConnections()

	return nil
}

// acceptConnections accepts incoming connections
func (s *Server) acceptConnections() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.shutdown:
				return
			default:
				s.logger.Error("Failed to accept connection", zap.Error(err))
				continue
			}
		}

		// Check connection limit
		s.mu.Lock()
		if len(s.conns) >= s.config.MaxConnections {
			s.mu.Unlock()
			s.logger.Warn("Max connections reached, rejecting",
				zap.String("remote", conn.RemoteAddr().String()))
			conn.Close()
			continue
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()

		// Handle connection
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection handles a single client connection
func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()

	remoteAddr := conn.RemoteAddr().String()
	s.logger.Debug("Client connected", zap.String("remote", remoteAddr))

	reader := NewRESPReader(conn)
	writer := NewRESPWriter(conn)

	// Set initial deadline
	if s.config.IdleTimeout > 0 {
		conn.SetDeadline(time.Now().Add(s.config.IdleTimeout))
	}

	for {
		// Check shutdown
		select {
		case <-s.shutdown:
			return
		default:
		}

		// Set read timeout
		if s.config.ReadTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))
		}

		// Read command
		val, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				s.logger.Debug("Client disconnected", zap.String("remote", remoteAddr))
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				s.logger.Debug("Connection timeout", zap.String("remote", remoteAddr))
				return
			}
			s.logger.Error("Failed to read command",
				zap.String("remote", remoteAddr),
				zap.Error(err))
			return
		}

		// Reset idle timeout
		if s.config.IdleTimeout > 0 {
			conn.SetDeadline(time.Now().Add(s.config.IdleTimeout))
		}

		// Parse command array
		args, err := val.ToArray()
		if err != nil {
			s.logger.Error("Invalid command format",
				zap.String("remote", remoteAddr),
				zap.Error(err))
			s.writeError(writer, err)
			continue
		}

		if len(args) == 0 {
			s.writeError(writer, errors.New("empty command"))
			continue
		}

		cmdName := args[0]
		cmdArgs := args[1:]

		s.logger.Debug("Executing command",
			zap.String("remote", remoteAddr),
			zap.String("command", cmdName),
			zap.Strings("args", cmdArgs))

		// Execute command
		ctx := context.Background()
		if s.config.WriteTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, s.config.WriteTimeout)
			defer cancel()
		}

		result, err := s.registry.Execute(ctx, cmdName, cmdArgs)
		if err != nil {
			s.logger.Error("Command execution failed",
				zap.String("remote", remoteAddr),
				zap.String("command", cmdName),
				zap.Error(err))
			s.writeError(writer, err)
			continue
		}

		// Write response
		if err := s.writeValue(writer, result); err != nil {
			s.logger.Error("Failed to write response",
				zap.String("remote", remoteAddr),
				zap.Error(err))
			return
		}

		if err := writer.Flush(); err != nil {
			s.logger.Error("Failed to flush response",
				zap.String("remote", remoteAddr),
				zap.Error(err))
			return
		}
	}
}

// writeValue writes a RESP value
func (s *Server) writeValue(writer *RESPWriter, val *Value) error {
	switch val.Type {
	case TypeSimpleString:
		return writer.WriteSimpleString(val.Str)
	case TypeError:
		return writer.WriteError(errors.New(val.Str))
	case TypeInteger:
		return writer.WriteInteger(val.Int)
	case TypeBulkString:
		if val.Null {
			return writer.WriteNull()
		}
		return writer.WriteBulkString(val.Str)
	case TypeArray:
		if val.Null {
			return writer.WriteNullArray()
		}
		if err := writer.WriteArray(len(val.Array)); err != nil {
			return err
		}
		for _, elem := range val.Array {
			if err := s.writeValue(writer, &elem); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported value type: %c", val.Type)
	}
}

// writeError writes an error response
func (s *Server) writeError(writer *RESPWriter, err error) {
	if writeErr := writer.WriteError(err); writeErr != nil {
		s.logger.Error("Failed to write error", zap.Error(writeErr))
	}
	if flushErr := writer.Flush(); flushErr != nil {
		s.logger.Error("Failed to flush error", zap.Error(flushErr))
	}
}

// Stop stops the KVP server gracefully
func (s *Server) Stop() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.shutdown)
	s.mu.Unlock()

	s.logger.Info("Stopping KVP server...")

	// Stop accepting new connections
	if s.listener != nil {
		s.listener.Close()
	}

	// Wait for existing connections with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("KVP server stopped gracefully")
	case <-time.After(s.config.ShutdownTimeout):
		s.logger.Warn("KVP server shutdown timeout, forcing close")
		// Close all connections
		s.mu.Lock()
		for conn := range s.conns {
			conn.Close()
		}
		s.mu.Unlock()
	}

	return nil
}

// Addr returns the server's listening address
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// ActiveConnections returns the number of active connections
func (s *Server) ActiveConnections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}
