package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJWTIssueAndVerify(t *testing.T) {
	issuer := NewJWTIssuer("test-secret-at-least-32-bytes-long-please", 5*time.Minute)
	uid := uuid.New()

	tok, exp, err := issuer.IssueAccessToken(uid)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if !exp.After(time.Now()) {
		t.Fatalf("exp must be in the future: %v", exp)
	}

	got, err := issuer.VerifyAccessToken(context.Background(), tok)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if got != uid {
		t.Fatalf("sub mismatch: got %s want %s", got, uid)
	}
}

func TestJWTRejectsTamperedToken(t *testing.T) {
	issuer := NewJWTIssuer("test-secret-at-least-32-bytes-long-please", 5*time.Minute)
	tok, _, err := issuer.IssueAccessToken(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.VerifyAccessToken(context.Background(), tok+"x"); err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestJWTRejectsDifferentSecret(t *testing.T) {
	issuer := NewJWTIssuer("test-secret-at-least-32-bytes-long-please", 5*time.Minute)
	other := NewJWTIssuer("different-secret-but-also-at-least-32-bytes!!", 5*time.Minute)
	tok, _, err := issuer.IssueAccessToken(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.VerifyAccessToken(context.Background(), tok); err == nil {
		t.Fatal("expected error verifying token signed with different secret")
	}
}

func TestJWTRejectsExpiredToken(t *testing.T) {
	issuer := NewJWTIssuer("test-secret-at-least-32-bytes-long-please", -1*time.Minute)
	tok, _, err := issuer.IssueAccessToken(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.VerifyAccessToken(context.Background(), tok); err == nil {
		t.Fatal("expected error for expired token")
	}
}
