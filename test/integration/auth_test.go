package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/auth"
	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/server"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

// Integration tests for authentication with HTTP server
// These tests verify the complete authentication flow through the HTTP API

// Request/Response types (matching server package internal types)
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expires_at"`
	User      *auth.User `json:"user"`
}

// Test helper to create a test server
func createTestAuthServer(t *testing.T, store storage.Store, userMgr *auth.UserManager, apiKeyMgr *auth.APIKeyManager, jwtMgr *auth.JWTManager) *server.HTTPServer {
	t.Helper()
	logger, _ := observability.NewLogger("error")

	// Create a unique metrics instance per test to avoid registration conflicts
	// In production, there's only one server, but in tests we create multiple

	return server.NewHTTPServer(server.HTTPServerConfig{
		Addr:          ":0",
		Store:         store,
		Logger:        logger,
		Metrics:       nil, // Disable metrics in integration tests to avoid conflicts
		AuthEnabled:   true,
		UserManager:   userMgr,
		APIKeyManager: apiKeyMgr,
		JWTManager:    jwtMgr,
	})
}

func TestAuthIntegration_UserLoginAndJWT(t *testing.T) {
	// Setup
	store := storage.NewMemoryStore()
	defer store.Close()

	userMgr := auth.NewUserManager(store)
	apiKeyMgr := auth.NewAPIKeyManager(store)
	jwtMgr := auth.NewJWTManager("test-secret-key", 1*time.Hour)

	authServer := createTestAuthServer(t, store, userMgr, apiKeyMgr, jwtMgr)

	// Create admin user
	adminUser, err := userMgr.CreateUser("admin", "admin123", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	// Test 1: Login with valid credentials
	loginReq := loginRequest{
		Username: "admin",
		Password: "admin123",
	}
	body, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Login failed: expected status %d, got %d", http.StatusOK, rec.Code)
		t.Logf("Response body: %s", rec.Body.String())
		return
	}

	var loginResp loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&loginResp); err != nil {
		t.Fatalf("Failed to decode login response: %v", err)
	}

	if loginResp.Token == "" {
		t.Error("Login response missing JWT token")
	}

	if loginResp.User.ID != adminUser.ID {
		t.Errorf("Login response user ID = %v, want %v", loginResp.User.ID, adminUser.ID)
	}

	if loginResp.User.Username != "admin" {
		t.Errorf("Login response username = %v, want admin", loginResp.User.Username)
	}

	// Test 2: Login with invalid credentials
	invalidReq := loginRequest{
		Username: "admin",
		Password: "wrongpassword",
	}
	body, _ = json.Marshal(invalidReq)
	req = httptest.NewRequest("POST", "/auth/login", bytes.NewReader(body))
	rec = httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Invalid login: expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	// Test 3: Use JWT token to access protected endpoint
	token := loginResp.Token
	req = httptest.NewRequest("PUT", "/keys/test-key", bytes.NewReader([]byte("test-value")))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()

	// Use the full router which handles path variables
	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Errorf("Authenticated PUT failed: expected status 200/201, got %d", rec.Code)
		t.Logf("Response body: %s", rec.Body.String())
	}

	// Verify the key was actually stored
	value, err := store.Get(req.Context(), "test-key")
	if err != nil {
		t.Errorf("Failed to retrieve stored key: %v", err)
	}

	if string(value) != "test-value" {
		t.Errorf("Stored value = %v, want test-value", string(value))
	}
}

func TestAuthIntegration_APIKeyCreationAndUsage(t *testing.T) {
	// Setup
	store := storage.NewMemoryStore()
	defer store.Close()

	userMgr := auth.NewUserManager(store)
	apiKeyMgr := auth.NewAPIKeyManager(store)
	jwtMgr := auth.NewJWTManager("test-secret-key", 1*time.Hour)

	authServer := createTestAuthServer(t, store, userMgr, apiKeyMgr, jwtMgr)

	// Create admin user and get JWT
	adminUser, err := userMgr.CreateUser("admin", "admin123", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	adminToken, err := jwtMgr.GenerateToken(adminUser.ID, adminUser.Username, adminUser.Role)
	if err != nil {
		t.Fatalf("Failed to generate admin token: %v", err)
	}

	// Test 1: Create API key
	createReq := map[string]interface{}{
		"name":       "test-api-key",
		"role":       auth.RoleWrite,
		"expires_in": "1h",
	}
	body, _ := json.Marshal(createReq)
	req := httptest.NewRequest("POST", "/auth/api-keys", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Create API key failed: expected status %d, got %d", http.StatusCreated, rec.Code)
		t.Logf("Response body: %s", rec.Body.String())
		return
	}

	var apiKeyObj auth.APIKey
	if err := json.NewDecoder(rec.Body).Decode(&apiKeyObj); err != nil {
		t.Fatalf("Failed to decode create API key response: %v", err)
	}

	if apiKeyObj.Key == "" {
		t.Fatal("Create API key response missing key field")
	}

	if apiKeyObj.ID == "" {
		t.Fatal("Create API key response missing id field")
	}

	apiKey := apiKeyObj.Key
	apiKeyID := apiKeyObj.ID

	// Test 2: Use API key to access protected endpoint
	req = httptest.NewRequest("PUT", "/keys/api-test", bytes.NewReader([]byte("api-value")))
	req.Header.Set("X-API-Key", apiKey)
	rec = httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Errorf("API key authenticated PUT failed: expected status 200/201, got %d", rec.Code)
		t.Logf("Response body: %s", rec.Body.String())
	}

	// Verify the key was stored
	value, err := store.Get(req.Context(), "api-test")
	if err != nil {
		t.Errorf("Failed to retrieve stored key: %v", err)
	}

	if string(value) != "api-value" {
		t.Errorf("Stored value = %v, want api-value", string(value))
	}

	// Test 3: List API keys
	req = httptest.NewRequest("GET", "/auth/api-keys", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("List API keys failed: expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var listResp []interface{}
	if err := json.NewDecoder(rec.Body).Decode(&listResp); err != nil {
		t.Fatalf("Failed to decode list API keys response: %v", err)
	}

	if len(listResp) != 1 {
		t.Errorf("Expected 1 API key, got %d", len(listResp))
	}

	// Test 4: Revoke API key (using apiKeyID from earlier)
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/auth/api-keys/%s", apiKeyID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Errorf("Revoke API key failed: expected status 200/204, got %d", rec.Code)
		t.Logf("Response body: %s", rec.Body.String())
	}

	// Test 5: Try to use revoked API key (should fail)
	req = httptest.NewRequest("PUT", "/keys/revoked-test", bytes.NewReader([]byte("value")))
	req.Header.Set("X-API-Key", apiKey)
	rec = httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Revoked API key should fail: expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthIntegration_RBACEnforcement(t *testing.T) {
	// Setup
	store := storage.NewMemoryStore()
	defer store.Close()

	userMgr := auth.NewUserManager(store)
	apiKeyMgr := auth.NewAPIKeyManager(store)
	jwtMgr := auth.NewJWTManager("test-secret-key", 1*time.Hour)

	// Create users with different roles
	adminUser, _ := userMgr.CreateUser("admin", "admin123", auth.RoleAdmin)
	writeUser, _ := userMgr.CreateUser("writer", "writer123", auth.RoleWrite)
	readUser, _ := userMgr.CreateUser("reader", "reader123", auth.RoleRead)

	adminToken, _ := jwtMgr.GenerateToken(adminUser.ID, adminUser.Username, adminUser.Role)
	writeToken, _ := jwtMgr.GenerateToken(writeUser.ID, writeUser.Username, writeUser.Role)
	readToken, _ := jwtMgr.GenerateToken(readUser.ID, readUser.Username, readUser.Role)

	authServer := createTestAuthServer(t, store, userMgr, apiKeyMgr, jwtMgr)

	tests := []struct {
		name           string
		token          string
		method         string
		path           string
		body           string
		expectedStatus int
	}{
		{
			name:           "admin can write",
			token:          adminToken,
			method:         "PUT",
			path:           "/keys/test",
			body:           "test-value",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "admin can read",
			token:          adminToken,
			method:         "GET",
			path:           "/keys/test",
			body:           "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "write can write",
			token:          writeToken,
			method:         "PUT",
			path:           "/keys/test2",
			body:           "test-value",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "write can read",
			token:          writeToken,
			method:         "GET",
			path:           "/keys/test2",
			body:           "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "read can read",
			token:          readToken,
			method:         "GET",
			path:           "/keys/test",
			body:           "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "read cannot write",
			token:          readToken,
			method:         "PUT",
			path:           "/keys/test3",
			body:           "test-value",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader *bytes.Reader
			if tt.body != "" {
				bodyReader = bytes.NewReader([]byte(tt.body))
			} else {
				bodyReader = bytes.NewReader(nil)
			}

			req := httptest.NewRequest(tt.method, tt.path, bodyReader)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			rec := httptest.NewRecorder()

			authServer.Handler().ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d for %s %s",
					tt.expectedStatus, rec.Code, tt.method, tt.path)
				t.Logf("Response body: %s", rec.Body.String())
			}
		})
	}
}

func TestAuthIntegration_UnauthorizedAccess(t *testing.T) {
	// Setup
	store := storage.NewMemoryStore()
	defer store.Close()

	userMgr := auth.NewUserManager(store)
	apiKeyMgr := auth.NewAPIKeyManager(store)
	jwtMgr := auth.NewJWTManager("test-secret-key", 1*time.Hour)

	authServer := createTestAuthServer(t, store, userMgr, apiKeyMgr, jwtMgr)

	tests := []struct {
		name           string
		authHeader     string
		apiKeyHeader   string
		expectedStatus int
	}{
		{
			name:           "no authentication",
			authHeader:     "",
			apiKeyHeader:   "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid JWT token",
			authHeader:     "Bearer invalid-token-here",
			apiKeyHeader:   "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "malformed auth header",
			authHeader:     "InvalidFormat token",
			apiKeyHeader:   "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid API key",
			authHeader:     "",
			apiKeyHeader:   "rak_invalid_key",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/keys/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if tt.apiKeyHeader != "" {
				req.Header.Set("X-API-Key", tt.apiKeyHeader)
			}
			rec := httptest.NewRecorder()

			authServer.Handler().ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestAuthIntegration_DisabledUser(t *testing.T) {
	// Setup
	store := storage.NewMemoryStore()
	defer store.Close()

	userMgr := auth.NewUserManager(store)
	apiKeyMgr := auth.NewAPIKeyManager(store)
	jwtMgr := auth.NewJWTManager("test-secret-key", 1*time.Hour)

	authServer := createTestAuthServer(t, store, userMgr, apiKeyMgr, jwtMgr)

	// Create user and get token
	user, _ := userMgr.CreateUser("testuser", "password123", auth.RoleWrite)
	token, _ := jwtMgr.GenerateToken(user.ID, user.Username, user.Role)

	// Test 1: User can access with valid token
	req := httptest.NewRequest("GET", "/keys/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Errorf("Active user should be able to access: expected status 200/404, got %d", rec.Code)
	}

	// Disable the user
	if err := userMgr.DisableUser(user.ID); err != nil {
		t.Fatalf("Failed to disable user: %v", err)
	}

	// Test 2: Disabled user cannot login
	loginReq := loginRequest{
		Username: "testuser",
		Password: "password123",
	}
	body, _ := json.Marshal(loginReq)
	req = httptest.NewRequest("POST", "/auth/login", bytes.NewReader(body))
	rec = httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Disabled user login: expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthIntegration_TokenRefresh(t *testing.T) {
	// Setup
	store := storage.NewMemoryStore()
	defer store.Close()

	userMgr := auth.NewUserManager(store)
	apiKeyMgr := auth.NewAPIKeyManager(store)
	jwtMgr := auth.NewJWTManager("test-secret-key", 1*time.Hour)

	authServer := createTestAuthServer(t, store, userMgr, apiKeyMgr, jwtMgr)

	// Create user and get token
	user, _ := userMgr.CreateUser("testuser", "password123", auth.RoleWrite)
	originalToken, _ := jwtMgr.GenerateToken(user.ID, user.Username, user.Role)

	// Add delay to ensure timestamps differ
	time.Sleep(1100 * time.Millisecond)

	// Test: Refresh token
	req := httptest.NewRequest("POST", "/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+originalToken)
	rec := httptest.NewRecorder()

	authServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Token refresh failed: expected status %d, got %d", http.StatusOK, rec.Code)
		t.Logf("Response body: %s", rec.Body.String())
		return
	}

	var refreshResp loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&refreshResp); err != nil {
		t.Fatalf("Failed to decode refresh response: %v", err)
	}

	if refreshResp.Token == "" {
		t.Error("Refresh response missing new token")
	}

	if refreshResp.Token == originalToken {
		t.Error("Refreshed token should be different from original")
	}

	// Verify new token is valid
	claims, err := jwtMgr.ValidateToken(refreshResp.Token)
	if err != nil {
		t.Errorf("New token validation failed: %v", err)
	}

	if claims.UserID != user.ID {
		t.Errorf("New token user ID = %v, want %v", claims.UserID, user.ID)
	}
}

func TestAuthIntegration_ConcurrentAccess(t *testing.T) {
	// Setup
	store := storage.NewMemoryStore()
	defer store.Close()

	userMgr := auth.NewUserManager(store)
	apiKeyMgr := auth.NewAPIKeyManager(store)
	jwtMgr := auth.NewJWTManager("test-secret-key", 1*time.Hour)

	authServer := createTestAuthServer(t, store, userMgr, apiKeyMgr, jwtMgr)

	// Create user and token
	user, _ := userMgr.CreateUser("testuser", "password123", auth.RoleWrite)
	token, _ := jwtMgr.GenerateToken(user.ID, user.Username, user.Role)

	// Test concurrent authenticated requests
	const numRequests = 50
	done := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(n int) {
			key := fmt.Sprintf("concurrent-key-%d", n)
			value := fmt.Sprintf("value-%d", n)

			// PUT request
			req := httptest.NewRequest("PUT", "/keys/"+key, bytes.NewReader([]byte(value)))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			authServer.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
				t.Errorf("Concurrent PUT failed for key %s: status %d", key, rec.Code)
				done <- false
				return
			}

			// GET request
			req = httptest.NewRequest("GET", "/keys/"+key, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec = httptest.NewRecorder()

			authServer.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Concurrent GET failed for key %s: status %d", key, rec.Code)
				done <- false
				return
			}

			done <- true
		}(i)
	}

	// Wait for all requests to complete
	successCount := 0
	for i := 0; i < numRequests; i++ {
		if <-done {
			successCount++
		}
	}

	if successCount != numRequests {
		t.Errorf("Only %d/%d concurrent requests succeeded", successCount, numRequests)
	}
}
