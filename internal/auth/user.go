package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

// UserManager manages users
type UserManager struct {
	store storage.Store
	mu    sync.RWMutex
}

// NewUserManager creates a new user manager
func NewUserManager(store storage.Store) *UserManager {
	return &UserManager{
		store: store,
	}
}

// CreateUser creates a new user
func (m *UserManager) CreateUser(username string, password string, role Role) (*User, error) {
	if username == "" {
		return nil, fmt.Errorf("username cannot be empty")
	}

	if password == "" {
		return nil, fmt.Errorf("password cannot be empty")
	}

	if !role.IsValid() {
		return nil, fmt.Errorf("invalid role: %s", role)
	}

	// Check if user already exists
	existingUser, _ := m.GetUserByUsername(username)
	if existingUser != nil {
		return nil, fmt.Errorf("user already exists: %s", username)
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &User{
		ID:        uuid.New().String(),
		Username:  username,
		Role:      role,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Disabled:  false,
	}

	// Store user
	if err := m.storeUser(user); err != nil {
		return nil, fmt.Errorf("failed to store user: %w", err)
	}

	// Store password separately
	if err := m.storePassword(user.ID, string(passwordHash)); err != nil {
		// Clean up user
		ctx := context.Background()
		_ = m.store.Delete(ctx, m.userKey(user.ID))
		return nil, fmt.Errorf("failed to store password: %w", err)
	}

	return user, nil
}

// GetUser retrieves a user by ID
func (m *UserManager) GetUser(id string) (*User, error) {
	ctx := context.Background()
	data, err := m.store.Get(ctx, m.userKey(id))
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}

	return &user, nil
}

// GetUserByUsername retrieves a user by username
func (m *UserManager) GetUserByUsername(username string) (*User, error) {
	users, err := m.ListUsers()
	if err != nil {
		return nil, err
	}

	for _, user := range users {
		if user.Username == username {
			return user, nil
		}
	}

	return nil, fmt.Errorf("user not found: %s", username)
}

// ListUsers returns all users
func (m *UserManager) ListUsers() ([]*User, error) {
	ctx := context.Background()
	prefix := "auth:user:"
	keys, err := m.store.List(ctx, prefix, 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	users := make([]*User, 0, len(keys))
	for _, key := range keys {
		data, err := m.store.Get(ctx, key)
		if err != nil {
			continue
		}

		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			continue
		}

		users = append(users, &user)
	}

	return users, nil
}

// UpdateUserRole updates a user's role
func (m *UserManager) UpdateUserRole(id string, newRole Role) error {
	if !newRole.IsValid() {
		return fmt.Errorf("invalid role: %s", newRole)
	}

	user, err := m.GetUser(id)
	if err != nil {
		return err
	}

	user.Role = newRole
	user.UpdatedAt = time.Now()

	return m.storeUser(user)
}

// DisableUser disables a user
func (m *UserManager) DisableUser(id string) error {
	user, err := m.GetUser(id)
	if err != nil {
		return err
	}

	user.Disabled = true
	user.UpdatedAt = time.Now()

	return m.storeUser(user)
}

// EnableUser enables a user
func (m *UserManager) EnableUser(id string) error {
	user, err := m.GetUser(id)
	if err != nil {
		return err
	}

	user.Disabled = false
	user.UpdatedAt = time.Now()

	return m.storeUser(user)
}

// DeleteUser permanently deletes a user
func (m *UserManager) DeleteUser(id string) error {
	ctx := context.Background()

	// Delete user
	if err := m.store.Delete(ctx, m.userKey(id)); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	// Delete password
	if err := m.store.Delete(ctx, m.passwordKey(id)); err != nil {
		// Log but don't fail
		fmt.Printf("failed to delete password for user %s: %v\n", id, err)
	}

	return nil
}

// AuthenticateUser verifies username and password
func (m *UserManager) AuthenticateUser(username, password string) (*User, error) {
	user, err := m.GetUserByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: invalid credentials")
	}

	if user.Disabled {
		return nil, fmt.Errorf("authentication failed: user is disabled")
	}

	// Get password hash
	passwordHash, err := m.getPassword(user.ID)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("authentication failed: invalid credentials")
	}

	return user, nil
}

// ChangePassword changes a user's password
func (m *UserManager) ChangePassword(id string, oldPassword, newPassword string) error {
	user, err := m.GetUser(id)
	if err != nil {
		return err
	}

	// Verify old password
	passwordHash, err := m.getPassword(user.ID)
	if err != nil {
		return fmt.Errorf("failed to get current password: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(oldPassword)); err != nil {
		return fmt.Errorf("invalid current password")
	}

	// Hash new password
	newPasswordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), BcryptCost)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	// Store new password
	return m.storePassword(user.ID, string(newPasswordHash))
}

// storeUser stores a user in the backing store
func (m *UserManager) storeUser(user *User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	ctx := context.Background()
	return m.store.Put(ctx, m.userKey(user.ID), data)
}

// storePassword stores a password hash
func (m *UserManager) storePassword(userID, passwordHash string) error {
	ctx := context.Background()
	return m.store.Put(ctx, m.passwordKey(userID), []byte(passwordHash))
}

// getPassword retrieves a password hash
func (m *UserManager) getPassword(userID string) (string, error) {
	ctx := context.Background()
	data, err := m.store.Get(ctx, m.passwordKey(userID))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// userKey returns the storage key for a user
func (m *UserManager) userKey(id string) string {
	return "auth:user:" + id
}

// passwordKey returns the storage key for a user's password
func (m *UserManager) passwordKey(userID string) string {
	return "auth:password:" + userID
}
