package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTManager manages JWT tokens
type JWTManager struct {
	secretKey     []byte
	tokenDuration time.Duration
	issuer        string
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(secretKey string, tokenDuration time.Duration) *JWTManager {
	return &JWTManager{
		secretKey:     []byte(secretKey),
		tokenDuration: tokenDuration,
		issuer:        "raftkv",
	}
}

// GenerateToken creates a new JWT token for a user
func (m *JWTManager) GenerateToken(userID, username string, role Role) (string, error) {
	if !role.IsValid() {
		return "", fmt.Errorf("invalid role: %s", role)
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"role":     string(role),
		"iss":      m.issuer,
		"iat":      now.Unix(),
		"exp":      now.Add(m.tokenDuration).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(m.secretKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// ValidateToken validates a JWT token and returns the claims
func (m *JWTManager) ValidateToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secretKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Extract claims
	userID, ok := claims["user_id"].(string)
	if !ok {
		return nil, fmt.Errorf("missing user_id claim")
	}

	username, ok := claims["username"].(string)
	if !ok {
		return nil, fmt.Errorf("missing username claim")
	}

	roleStr, ok := claims["role"].(string)
	if !ok {
		return nil, fmt.Errorf("missing role claim")
	}

	role := Role(roleStr)
	if !role.IsValid() {
		return nil, fmt.Errorf("invalid role in token: %s", roleStr)
	}

	// Verify expiration
	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing exp claim")
	}

	if time.Now().Unix() > int64(exp) {
		return nil, fmt.Errorf("token has expired")
	}

	return &JWTClaims{
		UserID:   userID,
		Username: username,
		Role:     role,
	}, nil
}

// RefreshToken creates a new token from an existing valid token
func (m *JWTManager) RefreshToken(tokenString string) (string, error) {
	claims, err := m.ValidateToken(tokenString)
	if err != nil {
		return "", fmt.Errorf("cannot refresh invalid token: %w", err)
	}

	return m.GenerateToken(claims.UserID, claims.Username, claims.Role)
}
