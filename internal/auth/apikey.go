package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

const (
	// APIKeyLength is the length of generated API keys in bytes
	APIKeyLength = 32
	// APIKeyPrefix is prepended to all API keys for identification
	APIKeyPrefix = "rftkv_"
	// BcryptCost is the cost for bcrypt hashing
	BcryptCost = 12
)

// APIKeyManager manages API keys
type APIKeyManager struct {
	store storage.Store
	mu    sync.RWMutex
	// In-memory cache for fast lookup (keyHash -> APIKey)
	cache map[string]*APIKey
}

// NewAPIKeyManager creates a new API key manager
func NewAPIKeyManager(store storage.Store) *APIKeyManager {
	return &APIKeyManager{
		store: store,
		cache: make(map[string]*APIKey),
	}
}

// GenerateAPIKey creates a new API key
func (m *APIKeyManager) GenerateAPIKey(userID, name string, role Role, expiresIn time.Duration) (*APIKey, error) {
	if !role.IsValid() {
		return nil, fmt.Errorf("invalid role: %s", role)
	}

	// Generate random key
	keyBytes := make([]byte, APIKeyLength)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}

	// Encode as base64 and add prefix
	key := APIKeyPrefix + base64.URLEncoding.EncodeToString(keyBytes)

	// Hash the key for storage
	keyHash, err := bcrypt.GenerateFromPassword([]byte(key), BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash key: %w", err)
	}

	// Create API key object
	now := time.Now()
	apiKey := &APIKey{
		ID:        uuid.New().String(),
		Key:       key,
		KeyHash:   string(keyHash),
		UserID:    userID,
		Name:      name,
		Role:      role,
		CreatedAt: now,
		UpdatedAt: now,
		Disabled:  false,
	}

	if expiresIn > 0 {
		apiKey.ExpiresAt = time.Now().Add(expiresIn)
	}

	// Store in backing store
	if err := m.storeAPIKey(apiKey); err != nil {
		return nil, fmt.Errorf("failed to store API key: %w", err)
	}

	// Cache it
	m.mu.Lock()
	m.cache[apiKey.KeyHash] = apiKey
	m.mu.Unlock()

	return apiKey, nil
}

// ValidateAPIKey validates an API key and returns the associated key object
func (m *APIKeyManager) ValidateAPIKey(key string) (*APIKey, error) {
	if key == "" {
		return nil, fmt.Errorf("empty API key")
	}

	// List all API keys and check against them
	// This is a simple implementation; in production, you'd use indexed lookups
	keys, err := m.ListAPIKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to list API keys: %w", err)
	}

	for _, apiKey := range keys {
		// Check if the key matches the hash
		if err := bcrypt.CompareHashAndPassword([]byte(apiKey.KeyHash), []byte(key)); err == nil {
			// Key matches - check if it's valid
			if !apiKey.IsValid() {
				if apiKey.Disabled {
					return nil, fmt.Errorf("API key is disabled")
				}
				if apiKey.IsExpired() {
					return nil, fmt.Errorf("API key has expired")
				}
			}

			// Update last used timestamp
			apiKey.LastUsed = time.Now()
			apiKey.UpdatedAt = time.Now()
			if err := m.storeAPIKey(apiKey); err != nil {
				// Log error but don't fail validation
				fmt.Printf("failed to update last used time: %v\n", err)
			}

			return apiKey, nil
		}
	}

	return nil, fmt.Errorf("invalid API key")
}

// GetAPIKey retrieves an API key by ID
func (m *APIKeyManager) GetAPIKey(id string) (*APIKey, error) {
	ctx := context.Background()
	data, err := m.store.Get(ctx, m.keyPrefix(id))
	if err != nil {
		return nil, fmt.Errorf("API key not found: %w", err)
	}

	// Unmarshal from storage format
	var storage apiKeyStorage
	if err := json.Unmarshal(data, &storage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal API key: %w", err)
	}

	// Convert to APIKey (excluding plaintext Key)
	apiKey := &APIKey{
		ID:        storage.ID,
		KeyHash:   storage.KeyHash,
		UserID:    storage.UserID,
		Name:      storage.Name,
		Role:      storage.Role,
		CreatedAt: storage.CreatedAt,
		UpdatedAt: storage.UpdatedAt,
		ExpiresAt: storage.ExpiresAt,
		LastUsed:  storage.LastUsed,
		Disabled:  storage.Disabled,
	}

	return apiKey, nil
}

// ListAPIKeys returns all API keys
func (m *APIKeyManager) ListAPIKeys() ([]*APIKey, error) {
	ctx := context.Background()
	prefix := "auth:apikey:"
	keys, err := m.store.List(ctx, prefix, 1000) // Limit to 1000 keys
	if err != nil {
		return nil, fmt.Errorf("failed to list API keys: %w", err)
	}

	apiKeys := make([]*APIKey, 0, len(keys))
	for _, key := range keys {
		data, err := m.store.Get(ctx, key)
		if err != nil {
			continue // Skip keys that can't be read
		}

		// Unmarshal from storage format
		var storage apiKeyStorage
		if err := json.Unmarshal(data, &storage); err != nil {
			continue // Skip keys that can't be unmarshaled
		}

		// Convert to APIKey (excluding plaintext Key)
		apiKey := &APIKey{
			ID:        storage.ID,
			KeyHash:   storage.KeyHash,
			UserID:    storage.UserID,
			Name:      storage.Name,
			Role:      storage.Role,
			CreatedAt: storage.CreatedAt,
			UpdatedAt: storage.UpdatedAt,
			ExpiresAt: storage.ExpiresAt,
			LastUsed:  storage.LastUsed,
			Disabled:  storage.Disabled,
		}

		apiKeys = append(apiKeys, apiKey)
	}

	return apiKeys, nil
}

// ListAPIKeysByUser returns all API keys for a specific user
func (m *APIKeyManager) ListAPIKeysByUser(userID string) ([]*APIKey, error) {
	allKeys, err := m.ListAPIKeys()
	if err != nil {
		return nil, err
	}

	userKeys := make([]*APIKey, 0)
	for _, key := range allKeys {
		if key.UserID == userID {
			userKeys = append(userKeys, key)
		}
	}

	return userKeys, nil
}

// RevokeAPIKey disables an API key
func (m *APIKeyManager) RevokeAPIKey(id string) error {
	apiKey, err := m.GetAPIKey(id)
	if err != nil {
		return err
	}

	apiKey.Disabled = true
	apiKey.UpdatedAt = time.Now()

	if err := m.storeAPIKey(apiKey); err != nil {
		return fmt.Errorf("failed to revoke API key: %w", err)
	}

	// Remove from cache
	m.mu.Lock()
	delete(m.cache, apiKey.KeyHash)
	m.mu.Unlock()

	return nil
}

// DeleteAPIKey permanently deletes an API key
func (m *APIKeyManager) DeleteAPIKey(id string) error {
	apiKey, err := m.GetAPIKey(id)
	if err != nil {
		return err
	}

	ctx := context.Background()
	if err := m.store.Delete(ctx, m.keyPrefix(id)); err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}

	// Remove from cache
	m.mu.Lock()
	delete(m.cache, apiKey.KeyHash)
	m.mu.Unlock()

	return nil
}

// apiKeyStorage is the internal storage format that includes KeyHash
type apiKeyStorage struct {
	ID        string    `json:"id"`
	KeyHash   string    `json:"key_hash"` // Store the hash
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	LastUsed  time.Time `json:"last_used,omitempty"`
	Disabled  bool      `json:"disabled"`
}

// storeAPIKey stores an API key in the backing store
func (m *APIKeyManager) storeAPIKey(apiKey *APIKey) error {
	// Convert to storage format (includes KeyHash, excludes plaintext Key)
	storage := apiKeyStorage{
		ID:        apiKey.ID,
		KeyHash:   apiKey.KeyHash,
		UserID:    apiKey.UserID,
		Name:      apiKey.Name,
		Role:      apiKey.Role,
		CreatedAt: apiKey.CreatedAt,
		UpdatedAt: apiKey.UpdatedAt,
		ExpiresAt: apiKey.ExpiresAt,
		LastUsed:  apiKey.LastUsed,
		Disabled:  apiKey.Disabled,
	}

	data, err := json.Marshal(storage)
	if err != nil {
		return fmt.Errorf("failed to marshal API key: %w", err)
	}

	ctx := context.Background()
	return m.store.Put(ctx, m.keyPrefix(apiKey.ID), data)
}

// keyPrefix returns the storage key prefix for an API key
func (m *APIKeyManager) keyPrefix(id string) string {
	return "auth:apikey:" + id
}

// LoadCache loads all API keys into memory cache
func (m *APIKeyManager) LoadCache() error {
	keys, err := m.ListAPIKeys()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, key := range keys {
		if key.IsValid() {
			m.cache[key.KeyHash] = key
		}
	}

	return nil
}
