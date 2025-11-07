package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/security"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

type HTTPServer struct {
	store   storage.Store
	router  *mux.Router
	server  *http.Server
	logger  *observability.Logger
	metrics *observability.Metrics

	// TLS configuration
	tlsEnabled bool
	certFile   string
	keyFile    string
}

type HTTPServerConfig struct {
	Addr            string
	Store           storage.Store
	Logger          *observability.Logger
	Metrics         *observability.Metrics
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MaxRequestSize  int64 // Maximum request body size (bytes)
	EnableRateLimit bool
	RateLimit       int // Requests per second

	// TLS configuration
	TLSConfig *security.TLSConfig // Optional TLS config (nil = HTTP, non-nil = HTTPS)
}

func NewHTTPServer(config HTTPServerConfig) *HTTPServer {
	// Defaults
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 10 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}
	if config.MaxRequestSize == 0 {
		config.MaxRequestSize = 1024 * 1024 // 1MB default
	}

	srv := &HTTPServer{
		store:   config.Store,
		logger:  config.Logger,
		metrics: config.Metrics,
	}

	// router
	router := mux.NewRouter()

	// Apply middleware
	router.Use(RecoveryMiddleware(config.Logger))
	router.Use(LoggingMiddleware(config.Logger))
	router.Use(MetricsMiddleware(config.Metrics))
	router.Use(CORSMiddleware())

	if config.EnableRateLimit {
		router.Use(RateLimitMiddleware(config.RateLimit))
	}

	// Register routes
	srv.registerRoutes(router, config.MaxRequestSize)

	srv.router = router
	srv.server = &http.Server{
		Addr:         config.Addr,
		Handler:      router,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}

	// Configure TLS if provided
	if config.TLSConfig != nil {
		// Validate TLS configuration first
		if err := security.ValidateTLSConfig(config.TLSConfig); err != nil {
			config.Logger.Error("Invalid TLS configuration", zap.Error(err))
			// Return error for invalid config in production
			// For now, just log and continue without TLS
		} else {
			tlsConfig, err := security.LoadServerTLSConfig(config.TLSConfig)
			if err != nil {
				config.Logger.Error("Failed to load TLS configuration", zap.Error(err))
			} else {
				srv.server.TLSConfig = tlsConfig
				srv.tlsEnabled = true
				srv.certFile = config.TLSConfig.CertFile
				srv.keyFile = config.TLSConfig.KeyFile
				config.Logger.Info("TLS enabled for HTTP server",
					zap.Bool("mtls", config.TLSConfig.EnableMTLS),
					zap.String("cert", config.TLSConfig.CertFile),
				)
			}
		}
	}

	return srv
}

// Handler returns the HTTP handler for the server (used for testing)
func (s *HTTPServer) Handler() http.Handler {
	return s.router
}

// registerRoutes registers all HTTP routes
func (s *HTTPServer) registerRoutes(router *mux.Router, maxRequestSize int64) {
	// Key operations
	router.HandleFunc("/keys/{key}", s.limitRequestSize(s.handleGet, maxRequestSize)).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/keys/{key}", s.limitRequestSize(s.handlePut, maxRequestSize)).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/keys/{key}", s.limitRequestSize(s.handleDelete, maxRequestSize)).Methods(http.MethodDelete, http.MethodOptions)
	router.HandleFunc("/keys", s.limitRequestSize(s.handleList, maxRequestSize)).Methods(http.MethodGet, http.MethodOptions)

	// Admin operations
	router.HandleFunc("/admin/snapshot", s.limitRequestSize(s.handleSnapshot, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)

	// Health and metrics
	router.HandleFunc("/health", s.handleHealth).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/ready", s.handleReady).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/stats", s.handleStats).Methods(http.MethodGet, http.MethodOptions)
	router.Handle("/metrics", promhttp.Handler()).Methods(http.MethodGet, http.MethodOptions)

	// Root
	router.HandleFunc("/", s.handleRoot).Methods(http.MethodGet, http.MethodOptions)
}

// limitRequestSize middleware limits request body size
func (s *HTTPServer) limitRequestSize(handler http.HandlerFunc, maxSize int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxSize)
		handler(w, r)
	}
}

// handleGet retrieves a value by key
func (s *HTTPServer) handleGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if key == "" {
		s.respondError(w, http.StatusBadRequest, "key is required")
		return
	}

	value, err := s.store.Get(r.Context(), key)
	if err == storage.ErrKeyNotFound {
		s.respondError(w, http.StatusNotFound, "key not found")
		return
	}
	if err != nil {
		s.logger.Error("Failed to get key",
			zap.String("key", key),
			zap.Error(err),
		)
		s.respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Return raw bytes
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write(value)
}

// handlePut stores a key-value pair
func (s *HTTPServer) handlePut(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if key == "" {
		s.respondError(w, http.StatusBadRequest, "key is required")
		return
	}

	// Read value from body
	value, err := io.ReadAll(r.Body)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	if err := s.store.Put(r.Context(), key, value); err != nil {
		s.logger.Error("Failed to put key",
			zap.String("key", key),
			zap.Error(err),
		)
		s.respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// handleDelete removes a key
func (s *HTTPServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if key == "" {
		s.respondError(w, http.StatusBadRequest, "key is required")
		return
	}

	if err := s.store.Delete(r.Context(), key); err != nil {
		s.logger.Error("Failed to delete key",
			zap.String("key", key),
			zap.Error(err),
		)
		s.respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleList lists keys by prefix
func (s *HTTPServer) handleList(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	limit := 1000 // Default limit

	// Parse limit from query
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	keys, err := s.store.List(r.Context(), prefix, limit)
	if err != nil {
		s.logger.Error("Failed to list keys",
			zap.String("prefix", prefix),
			zap.Error(err),
		)
		s.respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"keys":   keys,
		"count":  len(keys),
		"prefix": prefix,
		"limit":  limit,
	})
}

// handleSnapshot triggers a manual snapshot
func (s *HTTPServer) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	path, err := s.store.Snapshot(r.Context())
	if err != nil {
		s.logger.Error("Failed to create snapshot", zap.Error(err))
		s.respondError(w, http.StatusInternalServerError, "failed to create snapshot")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"snapshot": path,
		"message":  "snapshot created successfully",
	})
}

// handleHealth returns server health status
func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "healthy",
	})
}

// handleReady returns readiness status
func (s *HTTPServer) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check if store is accessible
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()

	_, err := s.store.List(ctx, "", 1)
	if err != nil {
		s.respondError(w, http.StatusServiceUnavailable, "store not ready")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ready",
	})
}

// handleStats returns store statistics
func (s *HTTPServer) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.store.Stats()

	response := map[string]interface{}{
		"gets":      stats.Gets,
		"puts":      stats.Puts,
		"deletes":   stats.Deletes,
		"key_count": stats.KeyCount,
	}

	// Add cache stats if available
	if stats.CacheHits > 0 || stats.CacheMisses > 0 {
		response["cache"] = map[string]interface{}{
			"hits":       stats.CacheHits,
			"misses":     stats.CacheMisses,
			"hit_rate":   stats.CacheHitRate,
			"size":       stats.CacheSize,
			"evictions":  stats.CacheEvictions,
		}
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleRoot returns API information
func (s *HTTPServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"service": "RaftKV",
		"version": "1.0.0",
		"endpoints": map[string]string{
			"GET /keys/{key}":      "Get value",
			"PUT /keys/{key}":      "Put value",
			"DELETE /keys/{key}":   "Delete key",
			"GET /keys?prefix=":    "List keys",
			"POST /admin/snapshot": "Create snapshot",
			"GET /health":          "Health check",
			"GET /ready":           "Readiness check",
			"GET /stats":           "Statistics",
			"GET /metrics":         "Prometheus metrics",
		},
	})
}

// respondJSON sends a JSON response
func (s *HTTPServer) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError sends an error response
func (s *HTTPServer) respondError(w http.ResponseWriter, status int, message string) {
	s.respondJSON(w, status, map[string]interface{}{
		"error": message,
	})
}

// Start starts the HTTP server (HTTP or HTTPS based on configuration)
func (s *HTTPServer) Start() error {
	if s.tlsEnabled {
		s.logger.Info("Starting HTTPS server",
			zap.String("addr", s.server.Addr),
			zap.String("cert", s.certFile),
		)
		return s.server.ListenAndServeTLS(s.certFile, s.keyFile)
	}

	s.logger.Info("Starting HTTP server (unencrypted)",
		zap.String("addr", s.server.Addr),
	)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down HTTP server")
	return s.server.Shutdown(ctx)
}
