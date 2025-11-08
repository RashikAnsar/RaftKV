package auth

import (
	"time"
)

// Role represents user access level
type Role string

const (
	RoleAdmin Role = "admin" // Full access to all operations
	RoleWrite Role = "write" // Read and write access to data
	RoleRead  Role = "read"  // Read-only access to data
)

// ValidRoles returns all valid role values
func ValidRoles() []Role {
	return []Role{RoleAdmin, RoleWrite, RoleRead}
}

// IsValid checks if the role is valid
func (r Role) IsValid() bool {
	for _, validRole := range ValidRoles() {
		if r == validRole {
			return true
		}
	}
	return false
}

// HasPermission checks if the role has a specific permission
func (r Role) HasPermission(requiredRole Role) bool {
	switch r {
	case RoleAdmin:
		return true // Admin has all permissions
	case RoleWrite:
		return requiredRole == RoleWrite || requiredRole == RoleRead
	case RoleRead:
		return requiredRole == RoleRead
	default:
		return false
	}
}

// User represents a user in the system
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Disabled  bool      `json:"disabled"`
}

// APIKey represents an API key
type APIKey struct {
	ID        string    `json:"id"`
	Key       string    `json:"key,omitempty"`       // Only shown on creation
	KeyHash   string    `json:"-"`                   // Never exposed
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`                // Description of the key
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"` // Optional expiration
	LastUsed  time.Time `json:"last_used,omitempty"`
	Disabled  bool      `json:"disabled"`
}

// IsExpired checks if the API key has expired
func (k *APIKey) IsExpired() bool {
	if k.ExpiresAt.IsZero() {
		return false // No expiration set
	}
	return time.Now().After(k.ExpiresAt)
}

// IsValid checks if the API key is valid for use
func (k *APIKey) IsValid() bool {
	return !k.Disabled && !k.IsExpired()
}

// JWTClaims represents JWT token claims
type JWTClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     Role   `json:"role"`
}

// AuthContext represents authenticated request context
type AuthContext struct {
	UserID   string
	Username string
	Role     Role
	Method   string // "api_key" or "jwt"
}
