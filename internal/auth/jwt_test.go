package auth

import (
	"strings"
	"testing"
	"time"
)

func TestJWTManager_GenerateToken(t *testing.T) {
	secret := "test-secret-key"
	manager := NewJWTManager(secret, 1*time.Hour)

	tests := []struct {
		name     string
		userID   string
		username string
		role     Role
		wantErr  bool
	}{
		{
			name:     "valid admin token",
			userID:   "user1",
			username: "admin",
			role:     RoleAdmin,
			wantErr:  false,
		},
		{
			name:     "valid write token",
			userID:   "user2",
			username: "writer",
			role:     RoleWrite,
			wantErr:  false,
		},
		{
			name:     "valid read token",
			userID:   "user3",
			username:   "reader",
			role:     RoleRead,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := manager.GenerateToken(tt.userID, tt.username, tt.role)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GenerateToken() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GenerateToken() unexpected error: %v", err)
				return
			}

			if token == "" {
				t.Error("GenerateToken() returned empty token")
			}

			// Token should have 3 parts (header.payload.signature)
			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				t.Errorf("GenerateToken() token has %d parts, want 3", len(parts))
			}

			// Verify we can validate the token
			claims, err := manager.ValidateToken(token)
			if err != nil {
				t.Errorf("ValidateToken() unexpected error: %v", err)
				return
			}

			if claims.UserID != tt.userID {
				t.Errorf("ValidateToken() userID = %v, want %v", claims.UserID, tt.userID)
			}

			if claims.Username != tt.username {
				t.Errorf("ValidateToken() username = %v, want %v", claims.Username, tt.username)
			}

			if claims.Role != tt.role {
				t.Errorf("ValidateToken() role = %v, want %v", claims.Role, tt.role)
			}
		})
	}
}

func TestJWTManager_ValidateToken(t *testing.T) {
	secret := "test-secret-key"
	manager := NewJWTManager(secret, 1*time.Hour)

	// Generate a valid token
	validToken, err := manager.GenerateToken("user1", "testuser", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	// Generate an expired token
	shortManager := NewJWTManager(secret, 1*time.Nanosecond)
	expiredToken, err := shortManager.GenerateToken("user2", "expireduser", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to generate expired token: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	// Generate a token with wrong secret
	wrongManager := NewJWTManager("wrong-secret", 1*time.Hour)
	wrongToken, err := wrongManager.GenerateToken("user3", "wronguser", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to generate wrong secret token: %v", err)
	}

	tests := []struct {
		name    string
		token   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid token",
			token:   validToken,
			wantErr: false,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
			errMsg:  "malformed",
		},
		{
			name:    "malformed token",
			token:   "not.a.valid.token",
			wantErr: true,
		},
		{
			name:    "expired token",
			token:   expiredToken,
			wantErr: true,
			errMsg:  "expired",
		},
		{
			name:    "wrong secret",
			token:   wrongToken,
			wantErr: true,
		},
		{
			name:    "random string",
			token:   "random-string-not-jwt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := manager.ValidateToken(tt.token)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateToken() expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateToken() error = %v, want to contain %v", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateToken() unexpected error: %v", err)
				return
			}

			if claims == nil {
				t.Error("ValidateToken() returned nil claims")
			}
		})
	}
}

func TestJWTManager_RefreshToken(t *testing.T) {
	secret := "test-secret-key"
	manager := NewJWTManager(secret, 1*time.Hour)

	// Generate original token
	originalToken, err := manager.GenerateToken("user1", "testuser", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	// Add a delay to ensure Unix timestamps differ (1 second resolution)
	time.Sleep(1100 * time.Millisecond)

	// Refresh the token
	newToken, err := manager.RefreshToken(originalToken)
	if err != nil {
		t.Errorf("RefreshToken() unexpected error: %v", err)
		return
	}

	if newToken == "" {
		t.Error("RefreshToken() returned empty token")
	}

	// New token should be different (different timestamps)
	if newToken == originalToken {
		t.Error("RefreshToken() returned same token")
	}

	// New token should have same claims
	originalClaims, err := manager.ValidateToken(originalToken)
	if err != nil {
		t.Fatalf("Failed to validate original token: %v", err)
	}

	newClaims, err := manager.ValidateToken(newToken)
	if err != nil {
		t.Fatalf("Failed to validate new token: %v", err)
	}

	if originalClaims.UserID != newClaims.UserID {
		t.Errorf("RefreshToken() userID changed: %v -> %v", originalClaims.UserID, newClaims.UserID)
	}

	if originalClaims.Username != newClaims.Username {
		t.Errorf("RefreshToken() username changed: %v -> %v", originalClaims.Username, newClaims.Username)
	}

	if originalClaims.Role != newClaims.Role {
		t.Errorf("RefreshToken() role changed: %v -> %v", originalClaims.Role, newClaims.Role)
	}

	// Both tokens should be valid (expiration details are internal to JWT)
	if newClaims.UserID == "" {
		t.Error("RefreshToken() new token has empty UserID")
	}
}

func TestJWTManager_RefreshToken_InvalidToken(t *testing.T) {
	secret := "test-secret-key"
	manager := NewJWTManager(secret, 1*time.Hour)

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
		{
			name:    "malformed token",
			token:   "not.a.valid.token",
			wantErr: true,
		},
		{
			name:    "random string",
			token:   "random-string",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.RefreshToken(tt.token)

			if tt.wantErr {
				if err == nil {
					t.Errorf("RefreshToken() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("RefreshToken() unexpected error: %v", err)
			}
		})
	}
}

func TestJWTManager_TokenExpiration(t *testing.T) {
	secret := "test-secret-key"
	shortExpiry := 2 * time.Second // Unix timestamps have 1-second resolution
	manager := NewJWTManager(secret, shortExpiry)

	// Generate token with short expiry
	token, err := manager.GenerateToken("user1", "testuser", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	// Token should be valid immediately
	claims, err := manager.ValidateToken(token)
	if err != nil {
		t.Errorf("ValidateToken() unexpected error immediately after creation: %v", err)
	}

	if claims == nil {
		t.Fatal("ValidateToken() returned nil claims")
	}

	// Wait for expiration (+ buffer for clock drift)
	time.Sleep(3 * time.Second)

	// Token should now be invalid
	_, err = manager.ValidateToken(token)
	if err == nil {
		t.Error("ValidateToken() expected error for expired token, got nil")
	}

	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("ValidateToken() error should mention expiration, got: %v", err)
	}
}

func TestJWTManager_DifferentSecrets(t *testing.T) {
	secret1 := "secret-one"
	secret2 := "secret-two"

	manager1 := NewJWTManager(secret1, 1*time.Hour)
	manager2 := NewJWTManager(secret2, 1*time.Hour)

	// Generate token with manager1
	token, err := manager1.GenerateToken("user1", "testuser", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	// manager1 should validate successfully
	_, err = manager1.ValidateToken(token)
	if err != nil {
		t.Errorf("manager1.ValidateToken() unexpected error: %v", err)
	}

	// manager2 should fail to validate (different secret)
	_, err = manager2.ValidateToken(token)
	if err == nil {
		t.Error("manager2.ValidateToken() expected error with different secret, got nil")
	}
}

func TestJWTClaims_Structure(t *testing.T) {
	// Test that JWTClaims can be created with all required fields
	claims := &JWTClaims{
		UserID:   "user1",
		Username: "testuser",
		Role:     RoleWrite,
	}

	if claims.UserID != "user1" {
		t.Errorf("UserID = %v, want user1", claims.UserID)
	}

	if claims.Username != "testuser" {
		t.Errorf("Username = %v, want testuser", claims.Username)
	}

	if claims.Role != RoleWrite {
		t.Errorf("Role = %v, want %v", claims.Role, RoleWrite)
	}
}

func TestJWTManager_TokenLifecycle(t *testing.T) {
	secret := "test-secret-key"
	manager := NewJWTManager(secret, 1*time.Hour)

	// 1. Generate token
	token, err := manager.GenerateToken("user1", "testuser", RoleWrite)
	if err != nil {
		t.Fatalf("GenerateToken() error: %v", err)
	}

	// 2. Validate token
	claims, err := manager.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error: %v", err)
	}

	if claims.UserID != "user1" {
		t.Errorf("ValidateToken() userID = %v, want user1", claims.UserID)
	}

	// 3. Refresh token
	newToken, err := manager.RefreshToken(token)
	if err != nil {
		t.Fatalf("RefreshToken() error: %v", err)
	}

	// 4. Validate new token
	newClaims, err := manager.ValidateToken(newToken)
	if err != nil {
		t.Fatalf("ValidateToken() error for new token: %v", err)
	}

	if newClaims.UserID != "user1" {
		t.Errorf("ValidateToken() new token userID = %v, want user1", newClaims.UserID)
	}

	// 5. Verify both tokens are valid
	if newClaims.Role != RoleWrite {
		t.Error("Refreshed token should maintain the same role")
	}
}

func TestNewJWTManager(t *testing.T) {
	tests := []struct {
		name       string
		secret     string
		expiry     time.Duration
		wantPanic  bool
	}{
		{
			name:      "valid config",
			secret:    "secret",
			expiry:    1 * time.Hour,
			wantPanic: false,
		},
		{
			name:      "empty secret",
			secret:    "",
			expiry:    1 * time.Hour,
			wantPanic: false, // Currently doesn't panic, but creates weak security
		},
		{
			name:      "zero expiry",
			secret:    "secret",
			expiry:    0,
			wantPanic: false,
		},
		{
			name:      "negative expiry",
			secret:    "secret",
			expiry:    -1 * time.Hour,
			wantPanic: false, // Tokens would be immediately expired
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if tt.wantPanic && r == nil {
					t.Error("NewJWTManager() expected panic, got none")
				}
				if !tt.wantPanic && r != nil {
					t.Errorf("NewJWTManager() unexpected panic: %v", r)
				}
			}()

			manager := NewJWTManager(tt.secret, tt.expiry)
			if manager == nil {
				t.Error("NewJWTManager() returned nil")
			}
		})
	}
}

func BenchmarkJWTManager_GenerateToken(b *testing.B) {
	secret := "test-secret-key"
	manager := NewJWTManager(secret, 1*time.Hour)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.GenerateToken("user1", "testuser", RoleWrite)
		if err != nil {
			b.Fatalf("GenerateToken() error: %v", err)
		}
	}
}

func BenchmarkJWTManager_ValidateToken(b *testing.B) {
	secret := "test-secret-key"
	manager := NewJWTManager(secret, 1*time.Hour)

	token, err := manager.GenerateToken("user1", "testuser", RoleWrite)
	if err != nil {
		b.Fatalf("GenerateToken() error: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.ValidateToken(token)
		if err != nil {
			b.Fatalf("ValidateToken() error: %v", err)
		}
	}
}

func BenchmarkJWTManager_RefreshToken(b *testing.B) {
	secret := "test-secret-key"
	manager := NewJWTManager(secret, 1*time.Hour)

	token, err := manager.GenerateToken("user1", "testuser", RoleWrite)
	if err != nil {
		b.Fatalf("GenerateToken() error: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		newToken, err := manager.RefreshToken(token)
		if err != nil {
			b.Fatalf("RefreshToken() error: %v", err)
		}
		token = newToken // Use new token for next iteration
	}
}
