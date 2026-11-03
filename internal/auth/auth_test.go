package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LinuxSuRen/face-recognition-server/internal/store"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	faceStore, err := store.Open(filepath.Join(t.TempDir(), "auth.sqlite3"))
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { faceStore.Close() })
	return &Service{Store: faceStore}
}

func TestLoginSessionAndLogout(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)

	hash, err := HashPassword("secret-1")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	if _, err := service.Store.CreateUser(ctx, "rick", "Rick", hash, AdminRole, true); err != nil {
		t.Fatalf("creating user: %v", err)
	}

	if _, _, err := service.Login(ctx, "rick", "wrong"); err != ErrInvalidCredentials {
		t.Fatalf("wrong password must fail, got %v", err)
	}
	token, user, err := service.Login(ctx, "rick", "secret-1")
	if err != nil || user.Username != "rick" || user.Role != AdminRole {
		t.Fatalf("login failed: %v %+v", err, user)
	}

	resolved, err := service.SessionUser(ctx, token)
	if err != nil || resolved.ID != user.ID {
		t.Fatalf("session resolve failed: %v %+v", err, resolved)
	}

	if err := service.Logout(ctx, token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := service.SessionUser(ctx, token); err == nil {
		t.Fatalf("logged-out session must not resolve")
	}
}

func TestDisabledUserCannotLogin(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)
	hash, _ := HashPassword("pw")
	user, err := service.Store.CreateUser(ctx, "bob", "Bob", hash, "user", true)
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	disabled := false
	if _, err := service.Store.UpdateUser(ctx, user.ID, store.UserUpdate{Enabled: &disabled}); err != nil {
		t.Fatalf("disabling: %v", err)
	}
	if _, _, err := service.Login(ctx, "bob", "pw"); err != ErrInvalidCredentials {
		t.Fatalf("disabled user must fail, got %v", err)
	}
}

func TestAPITokenIssueAndScope(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)

	raw, token, err := service.IssueToken(ctx, "系统A", []string{"verify", "verify", "bogus"})
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	if token.Scopes != "verify" {
		t.Fatalf("scopes normalized to %q, want verify only", token.Scopes)
	}

	resolved, err := service.AuthenticateBearer(ctx, raw, "verify")
	if err != nil || resolved.ID != token.ID {
		t.Fatalf("bearer verify failed: %v %+v", err, resolved)
	}
	if _, err := service.AuthenticateBearer(ctx, raw, "identify"); err != ErrInsufficientScope {
		t.Fatalf("missing scope must fail, got %v", err)
	}
	if _, err := service.AuthenticateBearer(ctx, "face_"+raw[5:]+"x", "verify"); err == nil {
		t.Fatalf("forged token must fail")
	}

	if err := service.Store.DeleteAPIToken(ctx, token.ID); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	if _, err := service.AuthenticateBearer(ctx, raw, "verify"); err == nil {
		t.Fatalf("revoked token must fail")
	}
}

func TestIssueTokenRequiresScopes(t *testing.T) {
	service := newTestService(t)
	if _, _, err := service.IssueToken(context.Background(), "empty", nil); err == nil {
		t.Fatalf("empty scopes must fail")
	}
}

func TestNormalizeScopesKeepsKnownOnly(t *testing.T) {
	got := NormalizeScopes([]string{"enroll", "nope", "identify", "verify", "enroll"})
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	if !HasScope(joinScopes(got), "enroll") || HasScope(joinScopes(got), "nope") {
		t.Fatalf("HasScope broken for %v", got)
	}
}

func joinScopes(scopes []string) string {
	result := ""
	for i, scope := range scopes {
		if i > 0 {
			result += ","
		}
		result += scope
	}
	return result
}
