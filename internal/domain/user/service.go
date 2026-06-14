package user

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,30}$`)

// ErrInvalidUsername is returned when the requested username is malformed.
var ErrInvalidUsername = errors.New("username must be 3–30 characters: letters, digits, underscores only")

// ErrInvalidCountry is returned for non-ISO-3166-1-alpha-2 country codes.
var ErrInvalidCountry = errors.New("country must be a 2-letter ISO 3166-1 alpha-2 code")

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// UpdateProfile validates params and delegates to the repository.
func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, params UpdateParams) (*User, error) {
	if err := validateUpdateParams(params); err != nil {
		return nil, err
	}
	u, err := s.repo.Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func validateUpdateParams(p UpdateParams) error {
	if p.Username != nil && *p.Username != "" {
		if !usernameRe.MatchString(*p.Username) {
			return ErrInvalidUsername
		}
	}
	if p.Country != nil && *p.Country != "" {
		c := strings.ToUpper(*p.Country)
		if len(c) != 2 {
			return ErrInvalidCountry
		}
		*p.Country = c
	}
	if p.Bio != nil && len(*p.Bio) > 280 {
		return errors.New("bio must be at most 280 characters")
	}
	if p.DisplayName != nil && len(*p.DisplayName) > 64 {
		return errors.New("display_name must be at most 64 characters")
	}
	return nil
}
