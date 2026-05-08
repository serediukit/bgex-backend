package friends

import (
	"time"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/domain/user"
)

// Status represents the state of a friend request.
type Status string

const (
	StatusPending  Status = "pending"
	StatusAccepted Status = "accepted"
	StatusDeclined Status = "declined"
)

// Request is a friend request row from the database.
type Request struct {
	ID          uuid.UUID `json:"id"`
	RequesterID uuid.UUID `json:"requester_id"`
	AddresseeID uuid.UUID `json:"addressee_id"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// RequestWithUser enriches a friend request with the other party's public profile.
type RequestWithUser struct {
	ID        uuid.UUID          `json:"id"`
	User      user.PublicProfile `json:"user"`
	Status    Status             `json:"status"`
	CreatedAt time.Time          `json:"created_at"`
}

// Friend represents an accepted friendship, embedding the friend's public profile.
type Friend struct {
	RequestID    uuid.UUID          `json:"request_id"`
	User         user.PublicProfile `json:"user"`
	FriendsSince time.Time          `json:"friends_since"`
}

// RelationshipKind describes the relationship between two users from one viewer's perspective.
type RelationshipKind string

const (
	KindNone            RelationshipKind = "none"
	KindPendingSent     RelationshipKind = "pending_sent"
	KindPendingReceived RelationshipKind = "pending_received"
	KindFriends         RelationshipKind = "friends"
)

// RelationshipStatus is returned by GetRelationshipStatus.
type RelationshipStatus struct {
	Status    RelationshipKind `json:"status"`
	RequestID *uuid.UUID       `json:"request_id,omitempty"`
}
