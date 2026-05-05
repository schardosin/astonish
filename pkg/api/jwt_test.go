package api

import (
	"testing"
	"time"
)

func TestJWTIssuer_AccessToken(t *testing.T) {
	issuer := NewJWTIssuer("test-secret-key-for-jwt-testing", 15*time.Minute, 90*24*time.Hour)

	tokenStr, err := issuer.IssueAccessToken("user-123", "alice@example.com", "Alice", "acme", "engineering", "admin", "")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected non-empty token")
	}

	// Validate the token
	claims, err := issuer.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}

	// Check claims
	if claims.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-123")
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", claims.Email, "alice@example.com")
	}
	if claims.DisplayName != "Alice" {
		t.Errorf("DisplayName = %q, want %q", claims.DisplayName, "Alice")
	}
	if claims.OrgSlug != "acme" {
		t.Errorf("OrgSlug = %q, want %q", claims.OrgSlug, "acme")
	}
	if claims.DefaultTeamSlug != "engineering" {
		t.Errorf("DefaultTeamSlug = %q, want %q", claims.DefaultTeamSlug, "engineering")
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want %q", claims.Role, "admin")
	}
	if claims.TokenType != TokenTypeAccess {
		t.Errorf("TokenType = %q, want %q", claims.TokenType, TokenTypeAccess)
	}
	if claims.Issuer != "astonish-platform" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "astonish-platform")
	}
	if claims.Subject != "user-123" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "user-123")
	}
}

func TestJWTIssuer_RefreshToken(t *testing.T) {
	issuer := NewJWTIssuer("test-secret-key-for-jwt-testing", 15*time.Minute, 90*24*time.Hour)

	tokenStr, err := issuer.IssueRefreshToken("user-123", "acme")
	if err != nil {
		t.Fatalf("IssueRefreshToken: %v", err)
	}

	// Validate as refresh token
	claims, err := issuer.ValidateRefreshToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-123")
	}
	if claims.OrgSlug != "acme" {
		t.Errorf("OrgSlug = %q, want %q", claims.OrgSlug, "acme")
	}
	if claims.TokenType != TokenTypeRefresh {
		t.Errorf("TokenType = %q, want %q", claims.TokenType, TokenTypeRefresh)
	}

	// Should fail when validated as access token
	_, err = issuer.ValidateAccessToken(tokenStr)
	if err == nil {
		t.Error("expected error validating refresh token as access token")
	}
}

func TestJWTIssuer_TokenExpiry(t *testing.T) {
	// Issue a token with 1ms TTL
	issuer := NewJWTIssuer("test-secret", time.Millisecond, time.Millisecond)

	tokenStr, err := issuer.IssueAccessToken("user-123", "", "", "acme", "", "", "")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	_, err = issuer.ValidateAccessToken(tokenStr)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestJWTIssuer_WrongSecret(t *testing.T) {
	issuer1 := NewJWTIssuer("secret-one", 15*time.Minute, 90*24*time.Hour)
	issuer2 := NewJWTIssuer("secret-two", 15*time.Minute, 90*24*time.Hour)

	tokenStr, err := issuer1.IssueAccessToken("user-123", "", "", "acme", "", "", "")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	// Should fail with different secret
	_, err = issuer2.ValidateAccessToken(tokenStr)
	if err == nil {
		t.Error("expected error validating token with wrong secret")
	}
}

func TestJWTIssuer_RandomSecretOnEmpty(t *testing.T) {
	// When secret is empty, a random key should be generated
	issuer := NewJWTIssuer("", 15*time.Minute, 90*24*time.Hour)

	tokenStr, err := issuer.IssueAccessToken("user-123", "", "", "acme", "", "", "")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	// Token should still validate with the same issuer instance
	claims, err := issuer.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-123")
	}
}

func TestJWTIssuer_TokenIDUniqueness(t *testing.T) {
	issuer := NewJWTIssuer("test-secret", 15*time.Minute, 90*24*time.Hour)

	token1, _ := issuer.IssueAccessToken("user-123", "", "", "", "", "", "")
	token2, _ := issuer.IssueAccessToken("user-123", "", "", "", "", "", "")

	claims1, _ := issuer.ValidateAccessToken(token1)
	claims2, _ := issuer.ValidateAccessToken(token2)

	if claims1.ID == claims2.ID {
		t.Error("expected different token IDs for separate issuances")
	}
}
