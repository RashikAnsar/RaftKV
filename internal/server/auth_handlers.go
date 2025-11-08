package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/RashikAnsar/raftkv/internal/auth"
)

// AuthHandlers handles authentication-related HTTP endpoints
type AuthHandlers struct {
	userManager   *auth.UserManager
	apiKeyManager *auth.APIKeyManager
	jwtManager    *auth.JWTManager
}

// NewAuthHandlers creates new authentication handlers
func NewAuthHandlers(userManager *auth.UserManager, apiKeyManager *auth.APIKeyManager, jwtManager *auth.JWTManager) *AuthHandlers {
	return &AuthHandlers{
		userManager:   userManager,
		apiKeyManager: apiKeyManager,
		jwtManager:    jwtManager,
	}
}

// RegisterRoutes registers authentication routes
func (h *AuthHandlers) RegisterRoutes(router *mux.Router, authMiddleware *auth.Middleware) {
	// Public routes (no authentication required)
	router.HandleFunc("/auth/login", h.handleLogin).Methods("POST")

	// Protected routes (require authentication)
	authRouter := router.PathPrefix("/auth").Subrouter()
	authRouter.Use(authMiddleware.Authenticate)

	// Token management
	authRouter.HandleFunc("/refresh", h.handleRefreshToken).Methods("POST")
	authRouter.HandleFunc("/logout", h.handleLogout).Methods("POST")

	// User management (admin only)
	adminRouter := authRouter.PathPrefix("/users").Subrouter()
	adminRouter.Use(authMiddleware.RequireAdmin())
	adminRouter.HandleFunc("", h.handleListUsers).Methods("GET")
	adminRouter.HandleFunc("", h.handleCreateUser).Methods("POST")
	adminRouter.HandleFunc("/{id}", h.handleGetUser).Methods("GET")
	adminRouter.HandleFunc("/{id}", h.handleDeleteUser).Methods("DELETE")
	adminRouter.HandleFunc("/{id}/role", h.handleUpdateUserRole).Methods("PUT")
	adminRouter.HandleFunc("/{id}/disable", h.handleDisableUser).Methods("POST")
	adminRouter.HandleFunc("/{id}/enable", h.handleEnableUser).Methods("POST")

	// API key management
	apiKeyRouter := authRouter.PathPrefix("/api-keys").Subrouter()
	apiKeyRouter.HandleFunc("", h.handleListAPIKeys).Methods("GET")
	apiKeyRouter.HandleFunc("", h.handleCreateAPIKey).Methods("POST")
	apiKeyRouter.HandleFunc("/{id}", h.handleGetAPIKey).Methods("GET")
	apiKeyRouter.HandleFunc("/{id}", h.handleRevokeAPIKey).Methods("DELETE")

	// Self-service routes
	authRouter.HandleFunc("/me", h.handleGetCurrentUser).Methods("GET")
	authRouter.HandleFunc("/password", h.handleChangePassword).Methods("PUT")
}

// Login request
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login response
type loginResponse struct {
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expires_at"`
	User      *auth.User `json:"user"`
}

// handleLogin handles user login and returns JWT token
func (h *AuthHandlers) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Authenticate user
	user, err := h.userManager.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}

	// Generate JWT token
	token, err := h.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Calculate expiration time (1 hour from now)
	expiresAt := time.Now().Add(1 * time.Hour)

	resp := loginResponse{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      user,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleRefreshToken refreshes a JWT token
func (h *AuthHandlers) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	authCtx := auth.GetAuthContext(r.Context())
	if authCtx == nil || authCtx.Method != "jwt" {
		http.Error(w, "JWT token required for refresh", http.StatusBadRequest)
		return
	}

	// Get user
	user, err := h.userManager.GetUser(authCtx.UserID)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Generate new token
	token, err := h.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	resp := loginResponse{
		Token:     token,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		User:      user,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleLogout handles user logout (client-side token invalidation)
func (h *AuthHandlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	// For JWT, logout is handled client-side by discarding the token
	// For API keys, we could revoke them here if needed
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Logged out successfully"})
}

// User creation request
type createUserRequest struct {
	Username string    `json:"username"`
	Password string    `json:"password"`
	Role     auth.Role `json:"role"`
}

// handleCreateUser creates a new user (admin only)
func (h *AuthHandlers) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	user, err := h.userManager.CreateUser(req.Username, req.Password, req.Role)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

// handleListUsers lists all users (admin only)
func (h *AuthHandlers) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userManager.ListUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

// handleGetUser gets a specific user (admin only)
func (h *AuthHandlers) handleGetUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	user, err := h.userManager.GetUser(id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// handleDeleteUser deletes a user (admin only)
func (h *AuthHandlers) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := h.userManager.DeleteUser(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Update role request
type updateRoleRequest struct {
	Role auth.Role `json:"role"`
}

// handleUpdateUserRole updates a user's role (admin only)
func (h *AuthHandlers) handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.userManager.UpdateUserRole(id, req.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, _ := h.userManager.GetUser(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// handleDisableUser disables a user (admin only)
func (h *AuthHandlers) handleDisableUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := h.userManager.DisableUser(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "User disabled"})
}

// handleEnableUser enables a user (admin only)
func (h *AuthHandlers) handleEnableUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := h.userManager.EnableUser(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "User enabled"})
}

// API key creation request
type createAPIKeyRequest struct {
	Name      string    `json:"name"`
	Role      auth.Role `json:"role"`
	ExpiresIn string    `json:"expires_in,omitempty"` // Duration string like "720h"
}

// handleCreateAPIKey creates a new API key
func (h *AuthHandlers) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	authCtx := auth.GetAuthContext(r.Context())
	if authCtx == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Parse expiration duration
	var expiresIn time.Duration
	if req.ExpiresIn != "" {
		var err error
		expiresIn, err = time.ParseDuration(req.ExpiresIn)
		if err != nil {
			http.Error(w, "Invalid expires_in duration", http.StatusBadRequest)
			return
		}
	}

	apiKey, err := h.apiKeyManager.GenerateAPIKey(authCtx.UserID, req.Name, req.Role, expiresIn)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(apiKey)
}

// handleListAPIKeys lists API keys for the current user
func (h *AuthHandlers) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	authCtx := auth.GetAuthContext(r.Context())
	if authCtx == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Admins can see all keys, others only their own
	var keys []*auth.APIKey
	var err error

	if authCtx.Role == auth.RoleAdmin {
		keys, err = h.apiKeyManager.ListAPIKeys()
	} else {
		keys, err = h.apiKeyManager.ListAPIKeysByUser(authCtx.UserID)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// handleGetAPIKey gets a specific API key
func (h *AuthHandlers) handleGetAPIKey(w http.ResponseWriter, r *http.Request) {
	authCtx := auth.GetAuthContext(r.Context())
	if authCtx == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	apiKey, err := h.apiKeyManager.GetAPIKey(id)
	if err != nil {
		http.Error(w, "API key not found", http.StatusNotFound)
		return
	}

	// Users can only see their own keys (unless admin)
	if authCtx.Role != auth.RoleAdmin && apiKey.UserID != authCtx.UserID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiKey)
}

// handleRevokeAPIKey revokes an API key
func (h *AuthHandlers) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	authCtx := auth.GetAuthContext(r.Context())
	if authCtx == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	apiKey, err := h.apiKeyManager.GetAPIKey(id)
	if err != nil {
		http.Error(w, "API key not found", http.StatusNotFound)
		return
	}

	// Users can only revoke their own keys (unless admin)
	if authCtx.Role != auth.RoleAdmin && apiKey.UserID != authCtx.UserID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := h.apiKeyManager.RevokeAPIKey(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetCurrentUser returns the currently authenticated user
func (h *AuthHandlers) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	authCtx := auth.GetAuthContext(r.Context())
	if authCtx == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	user, err := h.userManager.GetUser(authCtx.UserID)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// Change password request
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// handleChangePassword changes the current user's password
func (h *AuthHandlers) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	authCtx := auth.GetAuthContext(r.Context())
	if authCtx == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.userManager.ChangePassword(authCtx.UserID, req.OldPassword, req.NewPassword); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Password changed successfully"})
}
