// +build integration

package integration

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/kvp"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestKVPServer_BasicOperations tests basic KVP server operations
func TestKVPServer_BasicOperations(t *testing.T) {
	// Start KVP server
	store := storage.NewMemoryStore()
	defer store.Close()

	logger := zap.NewNop()
	config := kvp.ServerConfig{
		Addr:            ":0", // Random port
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		IdleTimeout:     1 * time.Minute,
		ShutdownTimeout: 5 * time.Second,
	}

	server := kvp.NewServer(config, store, logger)
	err := server.Start()
	require.NoError(t, err)
	defer server.Stop()

	// Get actual address
	addr := server.Addr()
	t.Logf("KVP server listening on %s", addr)

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Connect to server
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Test PING
	t.Run("PING", func(t *testing.T) {
		sendCommand(t, conn, []string{"PING"})
		resp := readResponse(t, reader)
		assert.Equal(t, "+PONG\r\n", resp)
	})

	// Test SET and GET
	t.Run("SET_GET", func(t *testing.T) {
		sendCommand(t, conn, []string{"SET", "mykey", "myvalue"})
		resp := readResponse(t, reader)
		assert.Equal(t, "+OK\r\n", resp)

		sendCommand(t, conn, []string{"GET", "mykey"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$7\r\nmyvalue\r\n", resp)
	})

	// Test non-existent key
	t.Run("GET_NonExistent", func(t *testing.T) {
		sendCommand(t, conn, []string{"GET", "nonexistent"})
		resp := readResponse(t, reader)
		assert.Equal(t, "$-1\r\n", resp) // Null bulk string
	})

	// Test DEL
	t.Run("DEL", func(t *testing.T) {
		sendCommand(t, conn, []string{"SET", "delkey", "value"})
		readResponse(t, reader) // Consume OK

		sendCommand(t, conn, []string{"DEL", "delkey"})
		resp := readResponse(t, reader)
		assert.Equal(t, ":1\r\n", resp) // Deleted 1 key

		sendCommand(t, conn, []string{"GET", "delkey"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$-1\r\n", resp) // Key no longer exists
	})

	// Test EXISTS
	t.Run("EXISTS", func(t *testing.T) {
		sendCommand(t, conn, []string{"SET", "existkey", "value"})
		readResponse(t, reader) // Consume OK

		sendCommand(t, conn, []string{"EXISTS", "existkey"})
		resp := readResponse(t, reader)
		assert.Equal(t, ":1\r\n", resp)

		sendCommand(t, conn, []string{"EXISTS", "nokey"})
		resp = readResponse(t, reader)
		assert.Equal(t, ":0\r\n", resp)
	})
}

// TestKVPServer_TTL tests TTL operations
func TestKVPServer_TTL(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	logger := zap.NewNop()
	config := kvp.ServerConfig{
		Addr:            ":0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		IdleTimeout:     1 * time.Minute,
		ShutdownTimeout: 5 * time.Second,
	}

	server := kvp.NewServer(config, store, logger)
	err := server.Start()
	require.NoError(t, err)
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", server.Addr())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Test SET with EX option
	t.Run("SET_EX", func(t *testing.T) {
		sendCommand(t, conn, []string{"SET", "exkey", "value", "EX", "2"})
		resp := readResponse(t, reader)
		assert.Equal(t, "+OK\r\n", resp)

		// Check TTL
		sendCommand(t, conn, []string{"TTL", "exkey"})
		resp = readResponse(t, reader)
		assert.Contains(t, resp, ":") // Should return seconds (1 or 2)
	})

	// Test EXPIRE
	t.Run("EXPIRE", func(t *testing.T) {
		sendCommand(t, conn, []string{"SET", "expirekey", "value"})
		readResponse(t, reader) // Consume OK

		sendCommand(t, conn, []string{"EXPIRE", "expirekey", "3"})
		resp := readResponse(t, reader)
		assert.Equal(t, ":1\r\n", resp) // Successfully set

		sendCommand(t, conn, []string{"TTL", "expirekey"})
		resp = readResponse(t, reader)
		assert.Contains(t, resp, ":") // Should have TTL
	})

	// Test TTL on key without expiration
	t.Run("TTL_NoExpiration", func(t *testing.T) {
		sendCommand(t, conn, []string{"SET", "noexpire", "value"})
		readResponse(t, reader) // Consume OK

		sendCommand(t, conn, []string{"TTL", "noexpire"})
		resp := readResponse(t, reader)
		assert.Equal(t, ":-1\r\n", resp) // No TTL
	})

	// Test TTL on non-existent key
	t.Run("TTL_NonExistent", func(t *testing.T) {
		sendCommand(t, conn, []string{"TTL", "nokey"})
		resp := readResponse(t, reader)
		assert.Equal(t, ":-2\r\n", resp) // Key doesn't exist
	})
}

// TestKVPServer_ListOperations tests list/queue operations
func TestKVPServer_ListOperations(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	logger := zap.NewNop()
	config := kvp.ServerConfig{
		Addr:            ":0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		IdleTimeout:     1 * time.Minute,
		ShutdownTimeout: 5 * time.Second,
	}

	server := kvp.NewServer(config, store, logger)
	err := server.Start()
	require.NoError(t, err)
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", server.Addr())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Test LPUSH and LPOP
	t.Run("LPUSH_LPOP", func(t *testing.T) {
		sendCommand(t, conn, []string{"LPUSH", "mylist", "a", "b", "c"})
		resp := readResponse(t, reader)
		assert.Equal(t, ":3\r\n", resp) // List length

		sendCommand(t, conn, []string{"LPOP", "mylist"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$1\r\nc\r\n", resp) // Last pushed (LIFO)

		sendCommand(t, conn, []string{"LPOP", "mylist"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$1\r\nb\r\n", resp)

		sendCommand(t, conn, []string{"LPOP", "mylist"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$1\r\na\r\n", resp)

		// List should be empty
		sendCommand(t, conn, []string{"LPOP", "mylist"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$-1\r\n", resp) // Null
	})

	// Test RPUSH and RPOP
	t.Run("RPUSH_RPOP", func(t *testing.T) {
		sendCommand(t, conn, []string{"RPUSH", "mylist2", "x", "y", "z"})
		resp := readResponse(t, reader)
		assert.Equal(t, ":3\r\n", resp)

		sendCommand(t, conn, []string{"RPOP", "mylist2"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$1\r\nz\r\n", resp) // Last pushed (LIFO from right)

		sendCommand(t, conn, []string{"RPOP", "mylist2"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$1\r\ny\r\n", resp)
	})
}

// TestKVPServer_KEYS tests the KEYS command
func TestKVPServer_KEYS(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	logger := zap.NewNop()
	config := kvp.ServerConfig{
		Addr:            ":0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		IdleTimeout:     1 * time.Minute,
		ShutdownTimeout: 5 * time.Second,
	}

	server := kvp.NewServer(config, store, logger)
	err := server.Start()
	require.NoError(t, err)
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", server.Addr())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Insert test data
	sendCommand(t, conn, []string{"SET", "user:1", "alice"})
	readResponse(t, reader)
	sendCommand(t, conn, []string{"SET", "user:2", "bob"})
	readResponse(t, reader)
	sendCommand(t, conn, []string{"SET", "product:1", "laptop"})
	readResponse(t, reader)

	// Test KEYS *
	t.Run("KEYS_All", func(t *testing.T) {
		sendCommand(t, conn, []string{"KEYS", "*"})
		resp := readResponse(t, reader)
		t.Logf("KEYS * response: %q", resp)
		assert.Contains(t, resp, "*3") // Array with 3 elements
	})

	// Test KEYS with prefix
	t.Run("KEYS_Prefix", func(t *testing.T) {
		sendCommand(t, conn, []string{"KEYS", "user:*"})
		resp := readResponse(t, reader)
		t.Logf("KEYS user:* response: %q", resp)
		assert.Contains(t, resp, "*2") // Array with 2 elements
	})
}

// TestKVPServer_MultipleConnections tests concurrent connections
func TestKVPServer_MultipleConnections(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	logger := zap.NewNop()
	config := kvp.ServerConfig{
		Addr:            ":0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		IdleTimeout:     1 * time.Minute,
		ShutdownTimeout: 5 * time.Second,
	}

	server := kvp.NewServer(config, store, logger)
	err := server.Start()
	require.NoError(t, err)
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	// Create 10 concurrent connections
	conns := make([]net.Conn, 10)
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("tcp", server.Addr())
		require.NoError(t, err)
		conns[i] = conn
		defer conn.Close()
	}

	// Each connection sets and gets its own key
	for i, conn := range conns {
		reader := bufio.NewReader(conn)
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)

		sendCommand(t, conn, []string{"SET", key, value})
		resp := readResponse(t, reader)
		assert.Equal(t, "+OK\r\n", resp)

		sendCommand(t, conn, []string{"GET", key})
		resp = readResponse(t, reader)
		expected := fmt.Sprintf("$%d\r\n%s\r\n", len(value), value)
		assert.Equal(t, expected, resp)
	}
}

// TestKVPServer_SetOptions tests SET command options
func TestKVPServer_SetOptions(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	logger := zap.NewNop()
	config := kvp.ServerConfig{
		Addr:            ":0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		IdleTimeout:     1 * time.Minute,
		ShutdownTimeout: 5 * time.Second,
	}

	server := kvp.NewServer(config, store, logger)
	err := server.Start()
	require.NoError(t, err)
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", server.Addr())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Test NX (only set if not exists)
	t.Run("SET_NX", func(t *testing.T) {
		sendCommand(t, conn, []string{"SET", "nxkey", "value1", "NX"})
		resp := readResponse(t, reader)
		assert.Equal(t, "+OK\r\n", resp) // Should succeed

		sendCommand(t, conn, []string{"SET", "nxkey", "value2", "NX"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$-1\r\n", resp) // Should fail (key exists)

		sendCommand(t, conn, []string{"GET", "nxkey"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$6\r\nvalue1\r\n", resp) // Still has original value
	})

	// Test XX (only set if exists)
	t.Run("SET_XX", func(t *testing.T) {
		sendCommand(t, conn, []string{"SET", "xxkey", "value", "XX"})
		resp := readResponse(t, reader)
		assert.Equal(t, "$-1\r\n", resp) // Should fail (doesn't exist)

		sendCommand(t, conn, []string{"SET", "xxkey", "value1"})
		readResponse(t, reader) // Consume OK

		sendCommand(t, conn, []string{"SET", "xxkey", "value2", "XX"})
		resp = readResponse(t, reader)
		assert.Equal(t, "+OK\r\n", resp) // Should succeed (exists)

		sendCommand(t, conn, []string{"GET", "xxkey"})
		resp = readResponse(t, reader)
		assert.Equal(t, "$6\r\nvalue2\r\n", resp) // Has updated value
	})
}

// TestKVPServer_GracefulShutdown tests server shutdown
func TestKVPServer_GracefulShutdown(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	logger := zap.NewNop()
	config := kvp.ServerConfig{
		Addr:            ":0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		IdleTimeout:     1 * time.Minute,
		ShutdownTimeout: 2 * time.Second,
	}

	server := kvp.NewServer(config, store, logger)
	err := server.Start()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Connect
	conn, err := net.Dial("tcp", server.Addr())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Send a command
	sendCommand(t, conn, []string{"PING"})
	resp := readResponse(t, reader)
	assert.Equal(t, "+PONG\r\n", resp)

	// Stop server
	err = server.Stop()
	assert.NoError(t, err)

	// Try to send another command (should fail)
	sendCommand(t, conn, []string{"PING"})
	_, err = readLine(reader)
	assert.Error(t, err) // Connection should be closed
}

// Helper functions

// sendCommand sends a RESP array command
func sendCommand(t *testing.T, conn net.Conn, args []string) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*%d\r\n", len(args)))
	for _, arg := range args {
		sb.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg))
	}
	_, err := conn.Write([]byte(sb.String()))
	require.NoError(t, err)
}

// readResponse reads a complete RESP response
func readResponse(t *testing.T, reader *bufio.Reader) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		var sb strings.Builder
		line, err := readLine(reader)
		if err != nil {
			errCh <- err
			return
		}
		sb.WriteString(line)

		// Parse response type
		if len(line) == 0 {
			done <- sb.String()
			return
		}

		switch line[0] {
		case '+', '-', ':':
			// Simple string, error, integer - single line
			done <- sb.String()
		case '$':
			// Bulk string
			var size int
			fmt.Sscanf(line, "$%d", &size)
			if size == -1 {
				done <- sb.String() // Null bulk string
				return
			}
			// Read size bytes + \r\n
			data := make([]byte, size+2)
			_, err := reader.Read(data)
			if err != nil {
				errCh <- err
				return
			}
			sb.Write(data)
			done <- sb.String()
		case '*':
			// Array - read count and then each element recursively
			var count int
			fmt.Sscanf(line, "*%d", &count)
			for i := 0; i < count; i++ {
				element, err := readLine(reader)
				if err != nil {
					errCh <- err
					return
				}
				sb.WriteString(element)
				// If bulk string, read the data
				if len(element) > 0 && element[0] == '$' {
					var size int
					fmt.Sscanf(element, "$%d", &size)
					if size > 0 {
						data := make([]byte, size+2)
						_, err := reader.Read(data)
						if err != nil {
							errCh <- err
							return
						}
						sb.Write(data)
					}
				}
			}
			done <- sb.String()
		default:
			done <- sb.String()
		}
	}()

	select {
	case <-ctx.Done():
		t.Fatal("timeout reading response")
		return ""
	case err := <-errCh:
		require.NoError(t, err)
		return ""
	case resp := <-done:
		return resp
	}
}

// readLine reads a line ending with \r\n
func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return line, nil
}
