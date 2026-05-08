package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/serediukit/bgex-backend/internal/domain/user"
)

const providerGoogle = "google"

// Tokens is the response envelope for issued auth credentials.
type Tokens struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	TokenType        string    `json:"token_type"`
}

// Service orchestrates auth flows.
type Service struct {
	users         *user.Repository
	repo          *Repository
	tokens        *RefreshTokenStore
	jwt           *JWTIssuer
	google        *GoogleOAuth
	refreshTTL    time.Duration
	oauthStateKey []byte
}

func NewService(users *user.Repository, repo *Repository, tokens *RefreshTokenStore, jwt *JWTIssuer, google *GoogleOAuth, refreshTTL time.Duration, oauthStateKey []byte) *Service {
	return &Service{
		users:         users,
		repo:          repo,
		tokens:        tokens,
		jwt:           jwt,
		google:        google,
		refreshTTL:    refreshTTL,
		oauthStateKey: oauthStateKey,
	}
}

// VerifyAccessToken implements middleware.AccessTokenVerifier.
func (s *Service) VerifyAccessToken(ctx context.Context, raw string) (uuid.UUID, error) {
	return s.jwt.VerifyAccessToken(ctx, raw)
}

// Register creates an email/password user and issues tokens.
func (s *Service) Register(ctx context.Context, email, password, displayName string) (*user.User, *Tokens, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}
	u, err := s.users.CreateWithPassword(ctx, user.Credentials{
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
	})
	if err != nil {
		return nil, nil, err
	}
	tokens, err := s.issueTokens(ctx, u.ID)
	if err != nil {
		return nil, nil, err
	}
	return u, tokens, nil
}

// Login authenticates by email + password and issues tokens.
func (s *Service) Login(ctx context.Context, email, password string) (*user.User, *Tokens, error) {
	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}
	hash, err := s.users.GetPasswordHash(ctx, u.ID)
	if err != nil {
		return nil, nil, err
	}
	if hash == "" {
		return nil, nil, ErrPasswordNotSet
	}
	if err := VerifyPassword(hash, password); err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	tokens, err := s.issueTokens(ctx, u.ID)
	if err != nil {
		return nil, nil, err
	}
	return u, tokens, nil
}

// Refresh rotates a refresh token: the presented one is revoked and a new pair is issued.
func (s *Service) Refresh(ctx context.Context, rawRefresh string) (*Tokens, error) {
	hash := hashRefreshToken(rawRefresh)
	userID, err := s.tokens.Consume(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrRefreshTokenInvalid) {
			return nil, ErrRefreshTokenInvalid
		}
		return nil, err
	}
	return s.issueTokens(ctx, userID)
}

// Logout revokes a presented refresh token.
func (s *Service) Logout(ctx context.Context, rawRefresh string) error {
	hash := hashRefreshToken(rawRefresh)
	return s.tokens.Revoke(ctx, hash)
}

// GoogleAuthURL returns a signed OAuth state (HMAC of a random nonce) and the redirect URL.
// The state cookie stored on the client must be presented back on callback.
func (s *Service) GoogleAuthURL() (authURL, state string, err error) {
	if s.google == nil {
		return "", "", ErrOAuthNotConfigured
	}
	state, err = s.signState()
	if err != nil {
		return "", "", err
	}
	return s.google.AuthURL(state), state, nil
}

// GoogleCallback validates the state, exchanges the code, upserts the user, and issues tokens.
func (s *Service) GoogleCallback(ctx context.Context, code, state, cookieState string) (*user.User, *Tokens, error) {
	if s.google == nil {
		return nil, nil, ErrOAuthNotConfigured
	}
	if state == "" || state != cookieState || !s.verifyState(state) {
		return nil, nil, ErrOAuthState
	}
	info, err := s.google.Exchange(ctx, code)
	if err != nil {
		return nil, nil, err
	}

	var u *user.User
	err = s.repo.WithTx(ctx, func(tx pgx.Tx) error {
		userID, findErr := s.repo.FindOAuthIdentity(ctx, tx, providerGoogle, info.Sub)
		if findErr == nil {
			u, err = s.loadUserInTx(ctx, tx, userID)
			return err
		}
		if !errors.Is(findErr, pgx.ErrNoRows) {
			return findErr
		}

		// No identity yet — try to find an existing user by email to link onto.
		existingID, findErr := s.repo.FindUserIDByEmail(ctx, tx, info.Email)
		switch {
		case findErr == nil:
			if err := s.repo.LinkOAuthIdentity(ctx, tx, existingID, providerGoogle, info.Sub); err != nil {
				return err
			}
			u, err = s.loadUserInTx(ctx, tx, existingID)
			return err
		case errors.Is(findErr, pgx.ErrNoRows):
			// Create a brand new user.
			created, cerr := s.users.CreateFromOAuth(ctx, tx, user.OAuthProfile{
				Email:         info.Email,
				EmailVerified: info.EmailVerified,
				DisplayName:   info.Name,
				AvatarURL:     info.Picture,
			})
			if cerr != nil {
				return cerr
			}
			if err := s.repo.LinkOAuthIdentity(ctx, tx, created.ID, providerGoogle, info.Sub); err != nil {
				return err
			}
			u = created
			return nil
		default:
			return findErr
		}
	})
	if err != nil {
		return nil, nil, err
	}

	tokens, err := s.issueTokens(ctx, u.ID)
	if err != nil {
		return nil, nil, err
	}
	return u, tokens, nil
}

func (s *Service) loadUserInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*user.User, error) {
	row := tx.QueryRow(ctx,
		`SELECT id, email, password_hash, username, display_name, avatar_url, bio, country, email_verified, created_at, updated_at FROM users WHERE id = $1`,
		id,
	)
	return scanUserRow(row)
}

func (s *Service) issueTokens(ctx context.Context, userID uuid.UUID) (*Tokens, error) {
	access, accessExp, err := s.jwt.IssueAccessToken(userID)
	if err != nil {
		return nil, err
	}
	rawRefresh, hash, err := generateRefreshToken()
	if err != nil {
		return nil, err
	}
	refreshExp := time.Now().UTC().Add(s.refreshTTL)
	if err := s.tokens.Store(ctx, userID, hash, s.refreshTTL); err != nil {
		return nil, err
	}
	return &Tokens{
		AccessToken:      access,
		AccessExpiresAt:  accessExp,
		RefreshToken:     rawRefresh,
		RefreshExpiresAt: refreshExp,
		TokenType:        "Bearer",
	}, nil
}

// --- OAuth state signing ---
// The state is `<nonce>.<hmac(nonce)>`. The cookie stores the same value; we
// verify the HMAC and then compare cookie vs. query-string on callback.

func (s *Service) signState() (string, error) {
	buf := make([]byte, 24)
	if _, err := secureRandom(buf); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	mac := hmac.New(sha256.New, s.oauthStateKey)
	mac.Write([]byte(nonce))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return nonce + "." + sig, nil
}

func (s *Service) verifyState(state string) bool {
	for i := 0; i < len(state); i++ {
		if state[i] == '.' {
			nonce := state[:i]
			sig := state[i+1:]
			mac := hmac.New(sha256.New, s.oauthStateKey)
			mac.Write([]byte(nonce))
			expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
			return hmac.Equal([]byte(expected), []byte(sig))
		}
	}
	return false
}

// scanUserRow mirrors user.scanRow for use inside transactions.
func scanUserRow(row pgx.Row) (*user.User, error) {
	var (
		u            user.User
		passwordHash *string
		username     *string
		displayName  *string
		avatarURL    *string
		bio          *string
		country      *string
	)
	if err := row.Scan(
		&u.ID, &u.Email, &passwordHash,
		&username, &displayName, &avatarURL, &bio, &country,
		&u.EmailVerified, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return nil, err
	}
	u.HasPassword = passwordHash != nil && *passwordHash != ""
	if username != nil {
		u.Username = *username
	}
	if displayName != nil {
		u.DisplayName = *displayName
	}
	if avatarURL != nil {
		u.AvatarURL = *avatarURL
	}
	if bio != nil {
		u.Bio = *bio
	}
	if country != nil {
		u.Country = *country
	}
	return &u, nil
}
