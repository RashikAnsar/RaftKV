package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

func TestAPIKeyManager_GenerateAPIKey(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	tests := []struct {
		name      string
		userID    string
		keyName   string
		role      Role
		expiresIn time.Duration
		wantErr   bool
	}{
		{
			name:      "valid admin key",
			userID:    "user1",
			keyName:   "admin-key",
			role:      RoleAdmin,
			expiresIn: 0,
			wantErr:   false,
		},
		{
			name:      "valid write key with expiry",
			userID:    "user2",
			keyName:   "write-key",
			role:      RoleWrite,
			expiresIn: 24 * time.Hour,
			wantErr:   false,
		},
		{
			name:      "valid read key",
			userID:    "user3",
			keyName:   "read-key",
			role:      RoleRead,
			expiresIn: 0,
			wantErr:   false,
		},
		{
			name:      "invalid role",
			userID:    "user4",
			keyName:   "invalid-key",
			role:      "invalid",
			expiresIn: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := manager.GenerateAPIKey(tt.userID, tt.keyName, tt.role, tt.expiresIn)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GenerateAPIKey() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GenerateAPIKey() unexpected error: %v", err)
				return
			}

			// Verify key structure
			if key.ID == "" {
				t.Error("GenerateAPIKey() ID is empty")
			}

			if !strings.HasPrefix(key.Key, APIKeyPrefix) {
				t.Errorf("GenerateAPIKey() key doesn't have prefix, got: %s", key.Key)
			}

			if key.KeyHash == "" {
				t.Error("GenerateAPIKey() KeyHash is empty")
			}

			if key.UserID != tt.userID {
				t.Errorf("GenerateAPIKey() userID = %v, want %v", key.UserID, tt.userID)
			}

			if key.Name != tt.keyName {
				t.Errorf("GenerateAPIKey() name = %v, want %v", key.Name, tt.keyName)
			}

			if key.Role != tt.role {
				t.Errorf("GenerateAPIKey() role = %v, want %v", key.Role, tt.role)
			}

			if key.Disabled {
				t.Error("GenerateAPIKey() key should not be disabled initially")
			}

			if tt.expiresIn > 0 {
				if key.ExpiresAt.IsZero() {
					t.Error("GenerateAPIKey() ExpiresAt should be set")
				}
			}

			// Verify key can be retrieved
			retrieved, err := manager.GetAPIKey(key.ID)
			if err != nil {
				t.Errorf("GetAPIKey() unexpected error: %v", err)
			}

			if retrieved.ID != key.ID {
				t.Errorf("GetAPIKey() ID = %v, want %v", retrieved.ID, key.ID)
			}

			if retrieved.KeyHash != key.KeyHash {
				t.Errorf("GetAPIKey() KeyHash mismatch")
			}
		})
	}
}

func TestAPIKeyManager_ValidateAPIKey(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	// Create a valid key
	key, err := manager.GenerateAPIKey("user1", "test-key", RoleWrite, 0)
	if err != nil {
		t.Fatalf("Failed to generate test key: %v", err)
	}

	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{
			name:    "valid key",
			key:     key.Key,
			wantErr: false,
		},
		{
			name:    "empty key",
			key:     "",
			wantErr: true,
		},
		{
			name:    "invalid key",
			key:     "invalid-key",
			wantErr: true,
		},
		{
			name:    "wrong prefix",
			key:     "wrong_prefix_key",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validated, err := manager.ValidateAPIKey(tt.key)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateAPIKey() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateAPIKey() unexpected error: %v", err)
				return
			}

			if validated.UserID != "user1" {
				t.Errorf("ValidateAPIKey() userID = %v, want user1", validated.UserID)
			}

			// LastUsed should be updated
			if validated.LastUsed.IsZero() {
				t.Error("ValidateAPIKey() LastUsed should be updated")
			}
		})
	}
}

func TestAPIKeyManager_ValidateExpiredKey(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	// Create a key that expires immediately
	key, err := manager.GenerateAPIKey("user1", "expired-key", RoleWrite, 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("Failed to generate test key: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	_, err = manager.ValidateAPIKey(key.Key)
	if err == nil {
		t.Error("ValidateAPIKey() expected error for expired key, got nil")
	}

	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("ValidateAPIKey() error should mention expiration, got: %v", err)
	}
}

func TestAPIKeyManager_ValidateDisabledKey(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	// Create and disable a key
	key, err := manager.GenerateAPIKey("user1", "disabled-key", RoleWrite, 0)
	if err != nil {
		t.Fatalf("Failed to generate test key: %v", err)
	}

	err = manager.RevokeAPIKey(key.ID)
	if err != nil {
		t.Fatalf("Failed to revoke key: %v", err)
	}

	_, err = manager.ValidateAPIKey(key.Key)
	if err == nil {
		t.Error("ValidateAPIKey() expected error for disabled key, got nil")
	}

	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("ValidateAPIKey() error should mention disabled, got: %v", err)
	}
}

func TestAPIKeyManager_ListAPIKeys(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	// Create multiple keys
	user1Keys := []string{"key1", "key2"}
	user2Keys := []string{"key3"}

	for _, name := range user1Keys {
		_, err := manager.GenerateAPIKey("user1", name, RoleWrite, 0)
		if err != nil {
			t.Fatalf("Failed to generate key %s: %v", name, err)
		}
	}

	for _, name := range user2Keys {
		_, err := manager.GenerateAPIKey("user2", name, RoleRead, 0)
		if err != nil {
			t.Fatalf("Failed to generate key %s: %v", name, err)
		}
	}

	// List all keys
	keys, err := manager.ListAPIKeys()
	if err != nil {
		t.Fatalf("ListAPIKeys() unexpected error: %v", err)
	}

	if len(keys) != 3 {
		t.Errorf("ListAPIKeys() count = %d, want 3", len(keys))
	}

	// List keys by user
	user1Keys_result, err := manager.ListAPIKeysByUser("user1")
	if err != nil {
		t.Fatalf("ListAPIKeysByUser() unexpected error: %v", err)
	}

	if len(user1Keys_result) != 2 {
		t.Errorf("ListAPIKeysByUser(user1) count = %d, want 2", len(user1Keys_result))
	}

	user2Keys_result, err := manager.ListAPIKeysByUser("user2")
	if err != nil {
		t.Fatalf("ListAPIKeysByUser() unexpected error: %v", err)
	}

	if len(user2Keys_result) != 1 {
		t.Errorf("ListAPIKeysByUser(user2) count = %d, want 1", len(user2Keys_result))
	}
}

func TestAPIKeyManager_RevokeAPIKey(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	// Create a key
	key, err := manager.GenerateAPIKey("user1", "revoke-test", RoleWrite, 0)
	if err != nil {
		t.Fatalf("Failed to generate test key: %v", err)
	}

	// Revoke it
	err = manager.RevokeAPIKey(key.ID)
	if err != nil {
		t.Fatalf("RevokeAPIKey() unexpected error: %v", err)
	}

	// Verify it's disabled
	retrieved, err := manager.GetAPIKey(key.ID)
	if err != nil {
		t.Fatalf("GetAPIKey() unexpected error: %v", err)
	}

	if !retrieved.Disabled {
		t.Error("RevokeAPIKey() key should be disabled")
	}

	// Verify it cannot be used
	_, err = manager.ValidateAPIKey(key.Key)
	if err == nil {
		t.Error("ValidateAPIKey() should fail for revoked key")
	}

	// Try to revoke non-existent key
	err = manager.RevokeAPIKey("non-existent-id")
	if err == nil {
		t.Error("RevokeAPIKey() should fail for non-existent key")
	}
}

func TestAPIKeyManager_DeleteAPIKey(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	// Create a key
	key, err := manager.GenerateAPIKey("user1", "delete-test", RoleWrite, 0)
	if err != nil {
		t.Fatalf("Failed to generate test key: %v", err)
	}

	// Delete it
	err = manager.DeleteAPIKey(key.ID)
	if err != nil {
		t.Fatalf("DeleteAPIKey() unexpected error: %v", err)
	}

	// Verify it's gone
	_, err = manager.GetAPIKey(key.ID)
	if err == nil {
		t.Error("GetAPIKey() should fail for deleted key")
	}

	// Try to delete non-existent key
	err = manager.DeleteAPIKey("non-existent-id")
	if err == nil {
		t.Error("DeleteAPIKey() should fail for non-existent key")
	}
}

func TestAPIKeyManager_GetAPIKey(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	// Try to get non-existent key
	_, err := manager.GetAPIKey("non-existent-id")
	if err == nil {
		t.Error("GetAPIKey() should fail for non-existent key")
	}

	// Create a key
	key, err := manager.GenerateAPIKey("user1", "get-test", RoleWrite, 0)
	if err != nil {
		t.Fatalf("Failed to generate test key: %v", err)
	}

	// Get it
	retrieved, err := manager.GetAPIKey(key.ID)
	if err != nil {
		t.Fatalf("GetAPIKey() unexpected error: %v", err)
	}

	// Verify fields
	if retrieved.ID != key.ID {
		t.Errorf("GetAPIKey() ID = %v, want %v", retrieved.ID, key.ID)
	}

	if retrieved.UserID != key.UserID {
		t.Errorf("GetAPIKey() UserID = %v, want %v", retrieved.UserID, key.UserID)
	}

	if retrieved.Name != key.Name {
		t.Errorf("GetAPIKey() Name = %v, want %v", retrieved.Name, key.Name)
	}

	if retrieved.Role != key.Role {
		t.Errorf("GetAPIKey() Role = %v, want %v", retrieved.Role, key.Role)
	}

	// KeyHash should be preserved (not the plaintext key)
	if retrieved.KeyHash != key.KeyHash {
		t.Error("GetAPIKey() KeyHash should be preserved")
	}

	// Plaintext key should not be in retrieved key
	if retrieved.Key != "" {
		t.Error("GetAPIKey() plaintext Key should be empty")
	}
}

func TestAPIKeyManager_ConcurrentOperations(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	// Create multiple keys concurrently
	concurrency := 10
	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(id int) {
			key, err := manager.GenerateAPIKey("user1", "concurrent-key", RoleWrite, 0)
			if err != nil {
				t.Errorf("GenerateAPIKey() concurrent error: %v", err)
				done <- false
				return
			}

			// Validate the key
			_, err = manager.ValidateAPIKey(key.Key)
			if err != nil {
				t.Errorf("ValidateAPIKey() concurrent error: %v", err)
				done <- false
				return
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < concurrency; i++ {
		<-done
	}

	// Verify all keys were created
	keys, err := manager.ListAPIKeys()
	if err != nil {
		t.Fatalf("ListAPIKeys() unexpected error: %v", err)
	}

	if len(keys) != concurrency {
		t.Errorf("ListAPIKeys() count = %d, want %d", len(keys), concurrency)
	}
}

func TestAPIKey_IsValid(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		apiKey   *APIKey
		expected bool
	}{
		{
			name: "valid key",
			apiKey: &APIKey{
				Disabled:  false,
				ExpiresAt: time.Time{}, // Zero time means no expiration
			},
			expected: true,
		},
		{
			name: "disabled key",
			apiKey: &APIKey{
				Disabled:  true,
				ExpiresAt: time.Time{},
			},
			expected: false,
		},
		{
			name: "expired key",
			apiKey: &APIKey{
				Disabled:  false,
				ExpiresAt: now.Add(-1 * time.Hour),
			},
			expected: false,
		},
		{
			name: "not yet expired key",
			apiKey: &APIKey{
				Disabled:  false,
				ExpiresAt: now.Add(1 * time.Hour),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.apiKey.IsValid()
			if result != tt.expected {
				t.Errorf("IsValid() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAPIKey_IsExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		apiKey   *APIKey
		expected bool
	}{
		{
			name: "no expiration",
			apiKey: &APIKey{
				ExpiresAt: time.Time{},
			},
			expected: false,
		},
		{
			name: "expired",
			apiKey: &APIKey{
				ExpiresAt: now.Add(-1 * time.Hour),
			},
			expected: true,
		},
		{
			name: "not expired",
			apiKey: &APIKey{
				ExpiresAt: now.Add(1 * time.Hour),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.apiKey.IsExpired()
			if result != tt.expected {
				t.Errorf("IsExpired() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAPIKeyManager_LoadCache(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	// Create some keys
	_, err := manager.GenerateAPIKey("user1", "cache-key1", RoleWrite, 0)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	_, err = manager.GenerateAPIKey("user2", "cache-key2", RoleRead, 0)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Create a disabled key (should not be cached)
	disabledKey, err := manager.GenerateAPIKey("user3", "disabled-key", RoleWrite, 0)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Disable it
	err = manager.RevokeAPIKey(disabledKey.ID)
	if err != nil {
		t.Fatalf("Failed to revoke key: %v", err)
	}

	// Load cache
	err = manager.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache() unexpected error: %v", err)
	}

	// Verify cache size (should only contain valid keys)
	manager.mu.RLock()
	cacheSize := len(manager.cache)
	manager.mu.RUnlock()

	if cacheSize != 2 {
		t.Errorf("LoadCache() cache size = %d, want 2 (disabled key should not be cached)", cacheSize)
	}
}

func BenchmarkAPIKeyManager_GenerateAPIKey(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.GenerateAPIKey("user1", "bench-key", RoleWrite, 0)
		if err != nil {
			b.Fatalf("GenerateAPIKey() error: %v", err)
		}
	}
}

func BenchmarkAPIKeyManager_ValidateAPIKey(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewAPIKeyManager(store)

	key, err := manager.GenerateAPIKey("user1", "bench-key", RoleWrite, 0)
	if err != nil {
		b.Fatalf("GenerateAPIKey() error: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.ValidateAPIKey(key.Key)
		if err != nil {
			b.Fatalf("ValidateAPIKey() error: %v", err)
		}
	}
}
