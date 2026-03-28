package server

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/observability"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	startTimeKey contextKey = "start_time"
)

type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

func LoggingMiddleware(logger *observability.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Generate request ID
			requestID := uuid.New().String()
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			ctx = context.WithValue(ctx, startTimeKey, time.Now())
			r = r.WithContext(ctx)

			rw := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK, // Default status
			}

			// Add request ID to response header
			rw.Header().Set("X-Request-ID", requestID)

			// Log request if logger is provided
			if logger != nil {
				logger.Info("HTTP request started",
					zap.String("request_id", requestID),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("remote_addr", r.RemoteAddr),
					zap.String("user_agent", r.UserAgent()),
				)
			}

			// Call next handler
			next.ServeHTTP(rw, r)

			// Log response if logger is provided
			if logger != nil {
				startTime, ok := ctx.Value(startTimeKey).(time.Time)
				if !ok {
					startTime = time.Now()
				}
				duration := time.Since(startTime)
				logger.Info("HTTP request completed",
					zap.String("request_id", requestID),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.Int("status", rw.statusCode),
					zap.Int("bytes", rw.bytesWritten),
					zap.Duration("duration", duration),
				)
			}
		})
	}
}

func MetricsMiddleware(metrics *observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rw := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(rw, r)

			// Record metrics if metrics object is provided
			if metrics != nil {
				duration := time.Since(start).Seconds()
				metrics.RecordHTTPRequest(
					r.Method,
					r.URL.Path,
					http.StatusText(rw.statusCode),
					duration,
					int(r.ContentLength),
					rw.bytesWritten,
				)
			}
		})
	}
}

func RecoveryMiddleware(logger *observability.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					requestID := GetRequestID(r.Context())
					if logger != nil {
						logger.Error("Panic recovered",
							zap.String("request_id", requestID),
							zap.Any("error", err),
							zap.Stack("stacktrace"),
						)
					}

					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware sets CORS headers. If allowedOrigins is non-empty, only matching
// origins receive the Access-Control-Allow-Origin header; otherwise "*" is used.
func CORSMiddleware(allowedOrigins ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if len(allowedOrigins) == 0 {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				for _, allowed := range allowedOrigins {
					if allowed == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Add("Vary", "Origin")
						break
					}
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ipBucket is a simple token-bucket rate limiter for a single IP.
type ipBucket struct {
	tokens    float64
	lastRefil time.Time
	rate      float64 // tokens per nanosecond
	burst     float64
}

func (b *ipBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(b.lastRefil).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.lastRefil = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimitMiddleware limits requests per IP per second using a token-bucket algorithm.
func RateLimitMiddleware(requestsPerSecond int) func(http.Handler) http.Handler {
	var (
		mu       sync.Mutex
		limiters = make(map[string]*ipBucket)
		rate     = float64(requestsPerSecond)
	)

	clientIP := func(r *http.Request) string {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			return fwd
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return ip
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)

			mu.Lock()
			b, ok := limiters[ip]
			if !ok {
				b = &ipBucket{
					tokens:    rate,
					lastRefil: time.Now(),
					rate:      rate / float64(time.Second),
					burst:     rate,
				}
				limiters[ip] = b
			}
			allowed := b.allow()
			mu.Unlock()

			if !allowed {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GetRequestID retrieves request ID from context
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}
