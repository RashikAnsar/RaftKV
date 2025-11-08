package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

func TestMiddleware_Authenticate_APIKey(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	// Create test API key
	apiKey, err := apiKeyMgr.GenerateAPIKey("user1", "test-key", RoleWrite, 0)
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	tests := []struct {
		name           string
		apiKey         string
		expectedStatus int
		expectAuth     bool
	}{
		{
			name:           "valid API key",
			apiKey:         apiKey.Key,
			expectedStatus: http.StatusOK,
			expectAuth:     true,
		},
		{
			name:           "invalid API key",
			apiKey:         "invalid-key",
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     false,
		},
		{
			name:           "empty API key header value",
			apiKey:         "",
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := GetAuthContext(r.Context())
				if ctx == nil && tt.expectAuth {
					t.Error("AuthContext should be set for valid auth")
				}
				if ctx != nil && !tt.expectAuth {
					t.Error("AuthContext should not be set for invalid auth")
				}

				if ctx != nil {
					if ctx.UserID != "user1" {
						t.Errorf("UserID = %v, want user1", ctx.UserID)
					}
					if ctx.Role != RoleWrite {
						t.Errorf("Role = %v, want %v", ctx.Role, RoleWrite)
					}
					if ctx.Method != "api_key" {
						t.Errorf("Method = %v, want api_key", ctx.Method)
					}
				}

				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Status = %v, want %v", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestMiddleware_Authenticate_JWT(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	// Create test JWT token
	token, err := jwtMgr.GenerateToken("user1", "testuser", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create JWT token: %v", err)
	}

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
		expectAuth     bool
	}{
		{
			name:           "valid JWT token",
			authHeader:     "Bearer " + token,
			expectedStatus: http.StatusOK,
			expectAuth:     true,
		},
		{
			name:           "invalid JWT token",
			authHeader:     "Bearer invalid-token",
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     false,
		},
		{
			name:           "malformed auth header",
			authHeader:     "InvalidFormat",
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     false,
		},
		{
			name:           "empty auth header",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := GetAuthContext(r.Context())
				if ctx == nil && tt.expectAuth {
					t.Error("AuthContext should be set for valid auth")
				}
				if ctx != nil && !tt.expectAuth {
					t.Error("AuthContext should not be set for invalid auth")
				}

				if ctx != nil {
					if ctx.UserID != "user1" {
						t.Errorf("UserID = %v, want user1", ctx.UserID)
					}
					if ctx.Username != "testuser" {
						t.Errorf("Username = %v, want testuser", ctx.Username)
					}
					if ctx.Role != RoleAdmin {
						t.Errorf("Role = %v, want %v", ctx.Role, RoleAdmin)
					}
					if ctx.Method != "jwt" {
						t.Errorf("Method = %v, want jwt", ctx.Method)
					}
				}

				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Status = %v, want %v", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestMiddleware_Authenticate_Disabled(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	// Create middleware with authentication disabled
	mw := NewMiddleware(apiKeyMgr, jwtMgr, false)

	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	// Don't set any auth headers

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should succeed even without auth when disabled
	if rr.Code != http.StatusOK {
		t.Errorf("Status = %v, want %v (auth disabled should allow access)", rr.Code, http.StatusOK)
	}
}

func TestMiddleware_RequireRole(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	// Create API keys with different roles
	adminKey, _ := apiKeyMgr.GenerateAPIKey("admin", "admin-key", RoleAdmin, 0)
	writeKey, _ := apiKeyMgr.GenerateAPIKey("writer", "write-key", RoleWrite, 0)
	readKey, _ := apiKeyMgr.GenerateAPIKey("reader", "read-key", RoleRead, 0)

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	tests := []struct {
		name           string
		apiKey         string
		requiredRole   Role
		expectedStatus int
	}{
		{
			name:           "admin can access admin endpoint",
			apiKey:         adminKey.Key,
			requiredRole:   RoleAdmin,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "admin can access write endpoint",
			apiKey:         adminKey.Key,
			requiredRole:   RoleWrite,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "admin can access read endpoint",
			apiKey:         adminKey.Key,
			requiredRole:   RoleRead,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "write cannot access admin endpoint",
			apiKey:         writeKey.Key,
			requiredRole:   RoleAdmin,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "write can access write endpoint",
			apiKey:         writeKey.Key,
			requiredRole:   RoleWrite,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "write can access read endpoint",
			apiKey:         writeKey.Key,
			requiredRole:   RoleRead,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "read cannot access admin endpoint",
			apiKey:         readKey.Key,
			requiredRole:   RoleAdmin,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "read cannot access write endpoint",
			apiKey:         readKey.Key,
			requiredRole:   RoleWrite,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "read can access read endpoint",
			apiKey:         readKey.Key,
			requiredRole:   RoleRead,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := mw.Authenticate(mw.RequireRole(tt.requiredRole)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})))

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("X-API-Key", tt.apiKey)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Status = %v, want %v", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestMiddleware_RequireRole_NoAuth(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	handler := mw.RequireRole(RoleRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Status = %v, want %v (should require auth)", rr.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_RequireAdmin(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	adminKey, _ := apiKeyMgr.GenerateAPIKey("admin", "admin-key", RoleAdmin, 0)
	writeKey, _ := apiKeyMgr.GenerateAPIKey("writer", "write-key", RoleWrite, 0)

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	tests := []struct {
		name           string
		apiKey         string
		expectedStatus int
	}{
		{
			name:           "admin can access",
			apiKey:         adminKey.Key,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "write cannot access",
			apiKey:         writeKey.Key,
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := mw.Authenticate(mw.RequireAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})))

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("X-API-Key", tt.apiKey)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Status = %v, want %v", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestMiddleware_RequireWrite(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	writeKey, _ := apiKeyMgr.GenerateAPIKey("writer", "write-key", RoleWrite, 0)
	readKey, _ := apiKeyMgr.GenerateAPIKey("reader", "read-key", RoleRead, 0)

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	tests := []struct {
		name           string
		apiKey         string
		expectedStatus int
	}{
		{
			name:           "write can access",
			apiKey:         writeKey.Key,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "read cannot access",
			apiKey:         readKey.Key,
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := mw.Authenticate(mw.RequireWrite()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})))

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("X-API-Key", tt.apiKey)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Status = %v, want %v", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestMiddleware_RequireRead(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	readKey, _ := apiKeyMgr.GenerateAPIKey("reader", "read-key", RoleRead, 0)

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	handler := mw.Authenticate(mw.RequireRead()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", readKey.Key)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Status = %v, want %v", rr.Code, http.StatusOK)
	}
}

func TestGetAuthContext(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	apiKey, _ := apiKeyMgr.GenerateAPIKey("user1", "test-key", RoleAdmin, 0)

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := GetAuthContext(r.Context())

		if ctx == nil {
			t.Error("GetAuthContext() returned nil for authenticated request")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if ctx.UserID == "" {
			t.Error("GetAuthContext() UserID is empty")
		}

		if ctx.Role == "" {
			t.Error("GetAuthContext() Role is empty")
		}

		if ctx.Method == "" {
			t.Error("GetAuthContext() Method is empty")
		}

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", apiKey.Key)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Status = %v, want %v", rr.Code, http.StatusOK)
	}
}

func TestGetAuthContext_NoAuth(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	ctx := GetAuthContext(req.Context())

	if ctx != nil {
		t.Error("GetAuthContext() should return nil when no auth context is set")
	}
}

func TestMiddleware_DisabledAPIKey(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	// Create and disable API key
	apiKey, err := apiKeyMgr.GenerateAPIKey("user1", "test-key", RoleWrite, 0)
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	err = apiKeyMgr.RevokeAPIKey(apiKey.ID)
	if err != nil {
		t.Fatalf("Failed to revoke API key: %v", err)
	}

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for disabled API key")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", apiKey.Key)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Status = %v, want %v (disabled key should be rejected)", rr.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_ExpiredJWT(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)

	// Create JWT manager with very short expiry
	jwtMgr := NewJWTManager("test-secret", 1*time.Second)

	token, err := jwtMgr.GenerateToken("user1", "testuser", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create JWT token: %v", err)
	}

	// Wait for token to expire
	time.Sleep(2 * time.Second)

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)

	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for expired token")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Status = %v, want %v (expired token should be rejected)", rr.Code, http.StatusUnauthorized)
	}
}

func BenchmarkMiddleware_Authenticate_APIKey(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	apiKey, _ := apiKeyMgr.GenerateAPIKey("user1", "test-key", RoleWrite, 0)

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)
	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", apiKey.Key)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkMiddleware_Authenticate_JWT(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	apiKeyMgr := NewAPIKeyManager(store)
	jwtMgr := NewJWTManager("test-secret", 1*time.Hour)

	token, _ := jwtMgr.GenerateToken("user1", "testuser", RoleWrite)

	mw := NewMiddleware(apiKeyMgr, jwtMgr, true)
	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}
