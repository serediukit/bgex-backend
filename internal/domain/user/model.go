package user

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	Username      string    `json:"username,omitempty"`
	DisplayName   string    `json:"display_name,omitempty"`
	AvatarURL     string    `json:"avatar_url,omitempty"`
	Bio           string    `json:"bio,omitempty"`
	Country       string    `json:"country,omitempty"`
	Role          string    `json:"role"`
	EmailVerified bool      `json:"email_verified"`
	HasPassword   bool      `json:"has_password"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// PublicProfile is the subset of User safe to expose to other users (no email).
type PublicProfile struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	Bio         string    `json:"bio,omitempty"`
	Country     string    `json:"country,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// UpdateParams carries the profile fields the user wants to change.
// A nil pointer means "no change"; an empty string pointer means "clear the field".
type UpdateParams struct {
	Username    *string
	DisplayName *string
	AvatarURL   *string
	Bio         *string
	Country     *string
}

// Credentials describes the data required to create a user via email/password.
type Credentials struct {
	Email        string
	PasswordHash string
	DisplayName  string
}

// OAuthProfile describes the data required to create a user from an OAuth
// provider's userinfo response.
type OAuthProfile struct {
	Email         string
	EmailVerified bool
	DisplayName   string
	AvatarURL     string
}

// ToPublic converts a User to its public-safe representation.
func (u *User) ToPublic() PublicProfile {
	return PublicProfile{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarURL,
		Bio:         u.Bio,
		Country:     u.Country,
		CreatedAt:   u.CreatedAt,
	}
}
