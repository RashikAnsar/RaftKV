package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const (
	// AuthContextKey is the context key for authentication info
	AuthContextKey contextKey = "auth_context"
)

// Middleware handles authentication for HTTP requests
type Middleware struct {
	apiKeyManager *APIKeyManager
	jwtManager    *JWTManager
	enabled       bool
}

// NewMiddleware creates a new authentication middleware
func NewMiddleware(apiKeyManager *APIKeyManager, jwtManager *JWTManager, enabled bool) *Middleware {
	return &Middleware{
		apiKeyManager: apiKeyManager,
		jwtManager:    jwtManager,
		enabled:       enabled,
	}
}

// Authenticate is the main authentication middleware
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication if disabled
		if !m.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Try API key authentication first
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "" {
			authCtx, err := m.authenticateAPIKey(apiKey)
			if err != nil {
				http.Error(w, "Invalid API key: "+err.Error(), http.StatusUnauthorized)
				return
			}

			// Add auth context to request
			ctx := context.WithValue(r.Context(), AuthContextKey, authCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Try JWT authentication
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			// Extract token from "Bearer <token>" format
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
				return
			}

			authCtx, err := m.authenticateJWT(token)
			if err != nil {
				http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}

			// Add auth context to request
			ctx := context.WithValue(r.Context(), AuthContextKey, authCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// No authentication provided
		http.Error(w, "Authentication required", http.StatusUnauthorized)
	})
}

// RequireRole creates middleware that requires a specific role
func (m *Middleware) RequireRole(role Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if authentication is disabled
			if !m.enabled {
				next.ServeHTTP(w, r)
				return
			}

			authCtx := GetAuthContext(r.Context())
			if authCtx == nil {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			if !authCtx.Role.HasPermission(role) {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// authenticateAPIKey validates an API key
func (m *Middleware) authenticateAPIKey(key string) (*AuthContext, error) {
	apiKey, err := m.apiKeyManager.ValidateAPIKey(key)
	if err != nil {
		return nil, err
	}

	return &AuthContext{
		UserID: apiKey.UserID,
		Role:   apiKey.Role,
		Method: "api_key",
	}, nil
}

// authenticateJWT validates a JWT token
func (m *Middleware) authenticateJWT(token string) (*AuthContext, error) {
	claims, err := m.jwtManager.ValidateToken(token)
	if err != nil {
		return nil, err
	}

	return &AuthContext{
		UserID:   claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
		Method:   "jwt",
	}, nil
}

// GetAuthContext retrieves authentication context from request context
func GetAuthContext(ctx context.Context) *AuthContext {
	authCtx, ok := ctx.Value(AuthContextKey).(*AuthContext)
	if !ok {
		return nil
	}
	return authCtx
}

// RequireAdmin is a helper to require admin role
func (m *Middleware) RequireAdmin() func(http.Handler) http.Handler {
	return m.RequireRole(RoleAdmin)
}

// RequireWrite is a helper to require write role or higher
func (m *Middleware) RequireWrite() func(http.Handler) http.Handler {
	return m.RequireRole(RoleWrite)
}

// RequireRead is a helper to require read role or higher (all authenticated users)
func (m *Middleware) RequireRead() func(http.Handler) http.Handler {
	return m.RequireRole(RoleRead)
}
