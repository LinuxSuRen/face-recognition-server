// Package auth implements platform accounts, browser sessions, external
// API tokens and the OIDC login flow (the home-gate patterns, scoped to
// this service).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/LinuxSuRen/face-recognition-server/internal/store"
)

// SessionCookie carries the browser session token.
const SessionCookie = "frs_session"

// SessionLifetime bounds browser logins.
const SessionLifetime = 7 * 24 * time.Hour

// AdminRole marks accounts that manage users and tokens.
const AdminRole = "admin"

// ErrInvalidCredentials marks a failed login.
var ErrInvalidCredentials = errors.New("用户名或密码不正确")

// ErrInsufficientScope marks a token missing the required scope.
var ErrInsufficientScope = errors.New("token lacks the required scope")

// Service wires the store into authentication flows.
type Service struct {
	Store *store.Store
}

// HashPassword bcrypt-hashes a password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// VerifyPassword checks a password against its hash.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Login verifies credentials and creates a session; the raw token is
// returned once for the cookie.
func (s *Service) Login(ctx context.Context, username, password string) (token string, user *store.User, err error) {
	creds, err := s.Store.GetUserForLogin(ctx, username)
	if err != nil {
		return "", nil, ErrInvalidCredentials
	}
	if !creds.Enabled || creds.PasswordHash == "" ||
		!VerifyPassword(creds.PasswordHash, password) {
		return "", nil, ErrInvalidCredentials
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return "", nil, err
	}
	if err := s.Store.CreateSession(ctx, tokenHash, creds.ID, SessionLifetime); err != nil {
		return "", nil, err
	}
	user, err = s.Store.GetUser(ctx, creds.ID)
	if err != nil {
		return "", nil, err
	}
	return token, user, nil
}

// SessionUser resolves a session cookie token to its account.
func (s *Service) SessionUser(ctx context.Context, token string) (*store.User, error) {
	if token == "" {
		return nil, store.ErrNotFound
	}
	session, err := s.Store.GetSession(ctx, hashToken(token))
	if err != nil {
		return nil, err
	}
	if time.Since(session.LastSeenAt) > time.Hour {
		_ = s.Store.TouchSession(ctx, session.TokenHash)
	}
	return s.Store.GetUser(ctx, session.UserID)
}

// Logout drops the session.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.Store.DeleteSession(ctx, hashToken(token))
}

// CreateSessionForUser opens a session programmatically (OIDC callback);
// the raw token is returned once for the cookie.
func (s *Service) CreateSessionForUser(ctx context.Context, userID int64) (string, error) {
	token, tokenHash, err := newToken()
	if err != nil {
		return "", err
	}
	if err := s.Store.CreateSession(ctx, tokenHash, userID, SessionLifetime); err != nil {
		return "", err
	}
	return token, nil
}

// ---- external API tokens ----

// IssueToken creates a bearer token for external systems; the raw value is
// returned exactly once, only its hash is stored.
func (s *Service) IssueToken(ctx context.Context, name string, scopes []string) (string, *store.APIToken, error) {
	cleaned := NormalizeScopes(scopes)
	if len(cleaned) == 0 {
		return "", nil, errors.New("at least one scope is required")
	}
	secret, hash, err := newToken()
	if err != nil {
		return "", nil, err
	}
	token, err := s.Store.CreateAPIToken(ctx, name, hash, strings.Join(cleaned, ","))
	if err != nil {
		return "", nil, err
	}
	return "face_" + secret, token, nil
}

// AuthenticateBearer validates an external token and its scope.
func (s *Service) AuthenticateBearer(ctx context.Context, rawToken, requiredScope string) (*store.APIToken, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(rawToken, "face_"))
	if raw == "" {
		return nil, ErrInvalidCredentials
	}
	token, err := s.Store.LookupAPIToken(ctx, hashToken(raw))
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if !HasScope(token.Scopes, requiredScope) {
		return nil, ErrInsufficientScope
	}
	if token.LastUsedAt == nil || time.Since(mustParseTime(*token.LastUsedAt)) > time.Minute {
		_ = s.Store.TouchAPIToken(ctx, token.ID)
	}
	return token, nil
}

// NormalizeScopes dedupes and orders the known scopes.
func NormalizeScopes(scopes []string) []string {
	allowed := []string{"verify", "identify", "enroll"}
	var result []string
	for _, candidate := range allowed {
		for _, scope := range scopes {
			if strings.TrimSpace(scope) == candidate {
				result = append(result, candidate)
				break
			}
		}
	}
	return result
}

// HasScope reports whether the csv scopes contain the wanted one.
func HasScope(csv, wanted string) bool {
	for _, scope := range strings.Split(csv, ",") {
		if strings.TrimSpace(scope) == wanted {
			return true
		}
	}
	return false
}

// newToken returns (raw, sha256-hex) of 32 random bytes.
func newToken() (string, string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", err
	}
	raw := hex.EncodeToString(buffer)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func mustParseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Now().UTC()
	}
	return parsed
}
