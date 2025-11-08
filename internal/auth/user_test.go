package auth

import (
	"strings"
	"testing"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

func TestUserManager_CreateUser(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	tests := []struct {
		name     string
		username string
		password string
		role     Role
		wantErr  bool
	}{
		{
			name:     "valid admin user",
			username: "admin",
			password: "password123",
			role:     RoleAdmin,
			wantErr:  false,
		},
		{
			name:     "valid write user",
			username: "writer",
			password: "password456",
			role:     RoleWrite,
			wantErr:  false,
		},
		{
			name:     "valid read user",
			username: "reader",
			password: "password789",
			role:     RoleRead,
			wantErr:  false,
		},
		{
			name:     "invalid role",
			username: "baduser",
			password: "password",
			role:     "invalid",
			wantErr:  true,
		},
		{
			name:     "empty username",
			username: "",
			password: "password",
			role:     RoleRead,
			wantErr:  true,
		},
		{
			name:     "empty password",
			username: "user",
			password: "",
			role:     RoleRead,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := manager.CreateUser(tt.username, tt.password, tt.role)

			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateUser() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("CreateUser() unexpected error: %v", err)
				return
			}

			if user.ID == "" {
				t.Error("CreateUser() ID is empty")
			}

			if user.Username != tt.username {
				t.Errorf("CreateUser() username = %v, want %v", user.Username, tt.username)
			}

			if user.Role != tt.role {
				t.Errorf("CreateUser() role = %v, want %v", user.Role, tt.role)
			}

			if user.Disabled {
				t.Error("CreateUser() user should not be disabled initially")
			}

			// Verify user can authenticate with the password
			_, err = manager.AuthenticateUser(tt.username, tt.password)
			if err != nil {
				t.Errorf("AuthenticateUser() should work with created password: %v", err)
			}
		})
	}
}

func TestUserManager_CreateUser_DuplicateUsername(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create first user
	_, err := manager.CreateUser("testuser", "password123", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create first user: %v", err)
	}

	// Try to create duplicate
	_, err = manager.CreateUser("testuser", "password456", RoleRead)
	if err == nil {
		t.Error("CreateUser() expected error for duplicate username, got nil")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("CreateUser() error should mention 'already exists', got: %v", err)
	}
}

func TestUserManager_AuthenticateUser(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create test user
	password := "testpassword123"
	user, err := manager.CreateUser("testuser", password, RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{
			name:     "valid credentials",
			username: "testuser",
			password: password,
			wantErr:  false,
		},
		{
			name:     "wrong password",
			username: "testuser",
			password: "wrongpassword",
			wantErr:  true,
		},
		{
			name:     "non-existent user",
			username: "nonexistent",
			password: password,
			wantErr:  true,
		},
		{
			name:     "empty username",
			username: "",
			password: password,
			wantErr:  true,
		},
		{
			name:     "empty password",
			username: "testuser",
			password: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authUser, err := manager.AuthenticateUser(tt.username, tt.password)

			if tt.wantErr {
				if err == nil {
					t.Errorf("AuthenticateUser() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("AuthenticateUser() unexpected error: %v", err)
				return
			}

			if authUser.ID != user.ID {
				t.Errorf("AuthenticateUser() ID = %v, want %v", authUser.ID, user.ID)
			}

			if authUser.Username != user.Username {
				t.Errorf("AuthenticateUser() username = %v, want %v", authUser.Username, user.Username)
			}
		})
	}
}

func TestUserManager_AuthenticateUser_DisabledUser(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create and disable user
	user, err := manager.CreateUser("disableduser", "password", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	err = manager.DisableUser(user.ID)
	if err != nil {
		t.Fatalf("Failed to disable user: %v", err)
	}

	// Try to authenticate
	_, err = manager.AuthenticateUser("disableduser", "password")
	if err == nil {
		t.Error("AuthenticateUser() expected error for disabled user, got nil")
	}

	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("AuthenticateUser() error should mention disabled, got: %v", err)
	}
}

func TestUserManager_GetUser(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create test user
	user, err := manager.CreateUser("testuser", "password", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Get existing user
	retrieved, err := manager.GetUser(user.ID)
	if err != nil {
		t.Errorf("GetUser() unexpected error: %v", err)
		return
	}

	if retrieved.ID != user.ID {
		t.Errorf("GetUser() ID = %v, want %v", retrieved.ID, user.ID)
	}

	if retrieved.Username != user.Username {
		t.Errorf("GetUser() username = %v, want %v", retrieved.Username, user.Username)
	}

	// Get non-existent user
	_, err = manager.GetUser("non-existent-id")
	if err == nil {
		t.Error("GetUser() expected error for non-existent user, got nil")
	}
}

func TestUserManager_GetUserByUsername(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create test user
	user, err := manager.CreateUser("testuser", "password", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Get existing user
	retrieved, err := manager.GetUserByUsername("testuser")
	if err != nil {
		t.Errorf("GetUserByUsername() unexpected error: %v", err)
		return
	}

	if retrieved.ID != user.ID {
		t.Errorf("GetUserByUsername() ID = %v, want %v", retrieved.ID, user.ID)
	}

	// Get non-existent user
	_, err = manager.GetUserByUsername("nonexistent")
	if err == nil {
		t.Error("GetUserByUsername() expected error for non-existent user, got nil")
	}
}

func TestUserManager_ListUsers(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create multiple users
	users := []string{"user1", "user2", "user3"}
	for _, username := range users {
		_, err := manager.CreateUser(username, "password", RoleWrite)
		if err != nil {
			t.Fatalf("Failed to create user %s: %v", username, err)
		}
	}

	// List all users
	allUsers, err := manager.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers() unexpected error: %v", err)
	}

	if len(allUsers) != 3 {
		t.Errorf("ListUsers() count = %d, want 3", len(allUsers))
	}
}

func TestUserManager_UpdateUserRole(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create test user
	user, err := manager.CreateUser("testuser", "password", RoleRead)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Update role
	err = manager.UpdateUserRole(user.ID, RoleWrite)
	if err != nil {
		t.Errorf("UpdateUserRole() unexpected error: %v", err)
		return
	}

	// Verify update
	updated, err := manager.GetUser(user.ID)
	if err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}

	if updated.Role != RoleWrite {
		t.Errorf("UpdateUserRole() role = %v, want %v", updated.Role, RoleWrite)
	}

	// Try to update with invalid role
	err = manager.UpdateUserRole(user.ID, "invalid")
	if err == nil {
		t.Error("UpdateUserRole() expected error for invalid role, got nil")
	}

	// Try to update non-existent user
	err = manager.UpdateUserRole("non-existent-id", RoleAdmin)
	if err == nil {
		t.Error("UpdateUserRole() expected error for non-existent user, got nil")
	}
}

func TestUserManager_ChangePassword(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create test user
	oldPassword := "oldpassword123"
	user, err := manager.CreateUser("testuser", oldPassword, RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Change password
	newPassword := "newpassword456"
	err = manager.ChangePassword(user.ID, oldPassword, newPassword)
	if err != nil {
		t.Errorf("ChangePassword() unexpected error: %v", err)
		return
	}

	// Verify old password doesn't work
	_, err = manager.AuthenticateUser("testuser", oldPassword)
	if err == nil {
		t.Error("AuthenticateUser() old password should not work")
	}

	// Verify new password works
	_, err = manager.AuthenticateUser("testuser", newPassword)
	if err != nil {
		t.Errorf("AuthenticateUser() new password should work, got error: %v", err)
	}

	// Try to change password with wrong old password
	err = manager.ChangePassword(user.ID, "wrongoldpass", "anotherpass")
	if err == nil {
		t.Error("ChangePassword() expected error for wrong old password, got nil")
	}

	// Try to change password for non-existent user
	err = manager.ChangePassword("non-existent-id", "oldpass", "newpass")
	if err == nil {
		t.Error("ChangePassword() expected error for non-existent user, got nil")
	}

	// Note: ChangePassword currently doesn't validate empty new password
	// This is a potential enhancement for the implementation
}

func TestUserManager_DisableUser(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create test user
	user, err := manager.CreateUser("testuser", "password", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Disable user
	err = manager.DisableUser(user.ID)
	if err != nil {
		t.Errorf("DisableUser() unexpected error: %v", err)
		return
	}

	// Verify user is disabled
	disabled, err := manager.GetUser(user.ID)
	if err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}

	if !disabled.Disabled {
		t.Error("DisableUser() user should be disabled")
	}

	// Try to disable non-existent user
	err = manager.DisableUser("non-existent-id")
	if err == nil {
		t.Error("DisableUser() expected error for non-existent user, got nil")
	}
}

func TestUserManager_EnableUser(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create and disable user
	user, err := manager.CreateUser("testuser", "password", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	err = manager.DisableUser(user.ID)
	if err != nil {
		t.Fatalf("DisableUser() error: %v", err)
	}

	// Enable user
	err = manager.EnableUser(user.ID)
	if err != nil {
		t.Errorf("EnableUser() unexpected error: %v", err)
		return
	}

	// Verify user is enabled
	enabled, err := manager.GetUser(user.ID)
	if err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}

	if enabled.Disabled {
		t.Error("EnableUser() user should be enabled")
	}

	// Try to enable non-existent user
	err = manager.EnableUser("non-existent-id")
	if err == nil {
		t.Error("EnableUser() expected error for non-existent user, got nil")
	}
}

func TestUserManager_DeleteUser(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create test user
	user, err := manager.CreateUser("testuser", "password", RoleWrite)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Delete user
	err = manager.DeleteUser(user.ID)
	if err != nil {
		t.Errorf("DeleteUser() unexpected error: %v", err)
		return
	}

	// Verify user is deleted
	_, err = manager.GetUser(user.ID)
	if err == nil {
		t.Error("GetUser() should fail for deleted user")
	}

	// Note: DeleteUser currently doesn't return error for non-existent user
	// It silently succeeds (idempotent delete)
}

func TestUserManager_ConcurrentOperations(t *testing.T) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create multiple users concurrently
	concurrency := 10
	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(id int) {
			username := "user" + string(rune('0'+id))
			user, err := manager.CreateUser(username, "password", RoleWrite)
			if err != nil {
				t.Errorf("CreateUser() concurrent error: %v", err)
				done <- false
				return
			}

			// Authenticate the user
			_, err = manager.AuthenticateUser(username, "password")
			if err != nil {
				t.Errorf("AuthenticateUser() concurrent error: %v", err)
				done <- false
				return
			}

			// Update the user role
			err = manager.UpdateUserRole(user.ID, RoleRead)
			if err != nil {
				t.Errorf("UpdateUserRole() concurrent error: %v", err)
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

	// Verify all users were created
	users, err := manager.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers() unexpected error: %v", err)
	}

	if len(users) != concurrency {
		t.Errorf("ListUsers() count = %d, want %d", len(users), concurrency)
	}
}

func TestUser_HasPermission(t *testing.T) {
	tests := []struct {
		name       string
		userRole   Role
		required   Role
		expected   bool
	}{
		{
			name:     "admin has admin permission",
			userRole: RoleAdmin,
			required: RoleAdmin,
			expected: true,
		},
		{
			name:     "admin has write permission",
			userRole: RoleAdmin,
			required: RoleWrite,
			expected: true,
		},
		{
			name:     "admin has read permission",
			userRole: RoleAdmin,
			required: RoleRead,
			expected: true,
		},
		{
			name:     "write has write permission",
			userRole: RoleWrite,
			required: RoleWrite,
			expected: true,
		},
		{
			name:     "write has read permission",
			userRole: RoleWrite,
			required: RoleRead,
			expected: true,
		},
		{
			name:     "write lacks admin permission",
			userRole: RoleWrite,
			required: RoleAdmin,
			expected: false,
		},
		{
			name:     "read has read permission",
			userRole: RoleRead,
			required: RoleRead,
			expected: true,
		},
		{
			name:     "read lacks write permission",
			userRole: RoleRead,
			required: RoleWrite,
			expected: false,
		},
		{
			name:     "read lacks admin permission",
			userRole: RoleRead,
			required: RoleAdmin,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.userRole.HasPermission(tt.required)
			if result != tt.expected {
				t.Errorf("HasPermission() = %v, want %v (role=%v, required=%v)",
					result, tt.expected, tt.userRole, tt.required)
			}
		})
	}
}

func BenchmarkUserManager_CreateUser(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		username := "user" + string(rune('0'+(i%10)))
		_, err := manager.CreateUser(username, "password", RoleWrite)
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			b.Fatalf("CreateUser() error: %v", err)
		}
	}
}

func BenchmarkUserManager_AuthenticateUser(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	manager := NewUserManager(store)

	// Create test user
	_, err := manager.CreateUser("benchuser", "password", RoleWrite)
	if err != nil {
		b.Fatalf("CreateUser() error: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.AuthenticateUser("benchuser", "password")
		if err != nil {
			b.Fatalf("AuthenticateUser() error: %v", err)
		}
	}
}
