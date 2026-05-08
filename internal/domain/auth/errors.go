package auth

import (
	"crypto/rand"
	"errors"
)

// Sentinel errors surfaced by the service layer.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrPasswordNotSet     = errors.New("password not set for this account (use OAuth sign-in)")
	ErrOAuthState         = errors.New("invalid oauth state")
	ErrOAuthNotConfigured = errors.New("oauth provider not configured")
)

// secureRandom is a thin wrapper so callers don't import crypto/rand directly
// for tests that want to inject a deterministic source.
var secureRandom = rand.Read
