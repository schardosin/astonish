package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Token types for the dual-token JWT system.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// PlatformClaims are the JWT claims for platform mode tokens.
// Access tokens are short-lived (15min) and contain the full user context.
// Refresh tokens are long-lived (90d) and contain only user identity.
type PlatformClaims struct {
	jwt.RegisteredClaims

	// TokenType distinguishes access tokens from refresh tokens.
	TokenType string `json:"typ"`

	// UserID is the platform user's unique identifier.
	UserID string `json:"uid"`

	// Email is the user's email address.
	Email string `json:"email,omitempty"`

	// DisplayName is the user's display name.
	DisplayName string `json:"name,omitempty"`

	// OrgSlug identifies the user's organization.
	OrgSlug string `json:"org,omitempty"`

	// DefaultTeamSlug is the user's default team within the org.
	// Can be overridden per-request via X-Astonish-Team header.
	DefaultTeamSlug string `json:"team,omitempty"`

	// Role is the user's role in the org (e.g., "admin", "member").
	Role string `json:"role,omitempty"`

	// PlatformRole is the user's platform-wide role (e.g., "superadmin").
	// Empty for regular users. Grants cross-org management capabilities.
	PlatformRole string `json:"prole,omitempty"`
}

// JWTIssuer generates and validates JWT tokens for platform mode.
type JWTIssuer struct {
	secret          []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	issuer          string
}

// NewJWTIssuer creates a new JWT issuer with the given secret.
// If secret is empty, a random 32-byte key is generated (tokens won't survive restarts).
func NewJWTIssuer(secret string, accessTTL, refreshTTL time.Duration) *JWTIssuer {
	var key []byte
	if secret != "" {
		key = []byte(secret)
	} else {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			// Fallback to a hex-encoded timestamp (not ideal but functional)
			key = []byte(fmt.Sprintf("astonish-fallback-%d", time.Now().UnixNano()))
		}
	}

	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	if refreshTTL <= 0 {
		refreshTTL = 90 * 24 * time.Hour
	}

	return &JWTIssuer{
		secret:          key,
		accessTokenTTL:  accessTTL,
		refreshTokenTTL: refreshTTL,
		issuer:          "astonish-platform",
	}
}

// IssueAccessToken creates a short-lived access token with full user context.
func (j *JWTIssuer) IssueAccessToken(userID, email, displayName, orgSlug, teamSlug, role, platformRole string) (string, error) {
	now := time.Now()
	claims := PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.accessTokenTTL)),
			ID:        generateTokenID(),
		},
		TokenType:       TokenTypeAccess,
		UserID:          userID,
		Email:           email,
		DisplayName:     displayName,
		OrgSlug:         orgSlug,
		DefaultTeamSlug: teamSlug,
		Role:            role,
		PlatformRole:    platformRole,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// IssueRefreshToken creates a long-lived refresh token with minimal claims.
func (j *JWTIssuer) IssueRefreshToken(userID, orgSlug, teamSlug string) (string, error) {
	now := time.Now()
	claims := PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.refreshTokenTTL)),
			ID:        generateTokenID(),
		},
		TokenType:       TokenTypeRefresh,
		UserID:          userID,
		OrgSlug:         orgSlug,
		DefaultTeamSlug: teamSlug,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// ValidateToken parses and validates a JWT token string.
// Returns the claims if valid, or an error if invalid/expired.
func (j *JWTIssuer) ValidateToken(tokenString string) (*PlatformClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &PlatformClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*PlatformClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// ValidateAccessToken validates a token and ensures it's an access token.
func (j *JWTIssuer) ValidateAccessToken(tokenString string) (*PlatformClaims, error) {
	claims, err := j.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != TokenTypeAccess {
		return nil, errors.New("not an access token")
	}
	return claims, nil
}

// ValidateRefreshToken validates a token and ensures it's a refresh token.
func (j *JWTIssuer) ValidateRefreshToken(tokenString string) (*PlatformClaims, error) {
	claims, err := j.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != TokenTypeRefresh {
		return nil, errors.New("not a refresh token")
	}
	return claims, nil
}

// AccessTokenTTL returns the access token time-to-live.
func (j *JWTIssuer) AccessTokenTTL() time.Duration {
	return j.accessTokenTTL
}

// RefreshTokenTTL returns the refresh token time-to-live.
func (j *JWTIssuer) RefreshTokenTTL() time.Duration {
	return j.refreshTokenTTL
}

// Sentinel errors for token validation.
var (
	ErrTokenExpired = errors.New("token expired")
)

// generateTokenID creates a random token ID for JWT jti claims.
func generateTokenID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
