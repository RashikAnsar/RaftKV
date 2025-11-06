package server

import (
	"context"
	"net/http"
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
				duration := time.Since(ctx.Value(startTimeKey).(time.Time))
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

func CORSMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
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

// RateLimitMiddleware limits requests per second (simple implementation)
func RateLimitMiddleware(requestsPerSecond int) func(http.Handler) http.Handler {
	ticker := time.NewTicker(time.Second / time.Duration(requestsPerSecond))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-ticker.C // Wait for token
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
