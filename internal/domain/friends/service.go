package friends

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Service implements business logic for the friends domain.
type Service struct {
	repo *Repository
}

// NewService creates a new Service with the given repository.
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// SendRequest sends a friend request from requesterID to addresseeID.
// Returns ErrSelfFriend if both IDs are equal.
func (s *Service) SendRequest(ctx context.Context, requesterID, addresseeID uuid.UUID) (*Request, error) {
	if requesterID == addresseeID {
		return nil, ErrSelfFriend
	}
	return s.repo.Send(ctx, requesterID, addresseeID)
}

// ListIncoming returns all pending friend requests directed at userID.
func (s *Service) ListIncoming(ctx context.Context, userID uuid.UUID) ([]RequestWithUser, error) {
	return s.repo.ListIncoming(ctx, userID)
}

// ListOutgoing returns all pending friend requests sent by userID.
func (s *Service) ListOutgoing(ctx context.Context, userID uuid.UUID) ([]RequestWithUser, error) {
	return s.repo.ListOutgoing(ctx, userID)
}

// Respond accepts or declines a pending friend request on behalf of userID.
// Returns ErrNotFound if the request does not exist, ErrForbidden if userID
// is not the addressee or the request is no longer pending.
func (s *Service) Respond(ctx context.Context, userID, requestID uuid.UUID, accept bool) (*Request, error) {
	req, err := s.repo.GetByID(ctx, requestID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if req.AddresseeID != userID || req.Status != StatusPending {
		return nil, ErrForbidden
	}

	newStatus := StatusDeclined
	if accept {
		newStatus = StatusAccepted
	}
	return s.repo.UpdateStatus(ctx, requestID, newStatus)
}

// CancelRequest cancels a pending friend request sent by userID.
// Returns ErrNotFound if the request does not exist, ErrForbidden if userID
// is not the requester or the request is no longer pending.
func (s *Service) CancelRequest(ctx context.Context, userID, requestID uuid.UUID) error {
	req, err := s.repo.GetByID(ctx, requestID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	if req.RequesterID != userID || req.Status != StatusPending {
		return ErrForbidden
	}

	return s.repo.Delete(ctx, requestID)
}

// ListFriends returns all accepted friends of userID.
func (s *Service) ListFriends(ctx context.Context, userID uuid.UUID) ([]Friend, error) {
	return s.repo.ListFriends(ctx, userID)
}

// Unfriend removes the friendship between userID and targetID in either direction.
func (s *Service) Unfriend(ctx context.Context, userID, targetID uuid.UUID) error {
	return s.repo.DeleteBetween(ctx, userID, targetID)
}

// GetRelationshipStatus returns the relationship between viewerID and targetID.
func (s *Service) GetRelationshipStatus(ctx context.Context, viewerID, targetID uuid.UUID) (RelationshipStatus, error) {
	req, err := s.repo.GetBetween(ctx, viewerID, targetID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return RelationshipStatus{Status: KindNone}, nil
		}
		return RelationshipStatus{}, err
	}

	id := req.ID
	rs := RelationshipStatus{RequestID: &id}

	switch req.Status {
	case StatusAccepted:
		rs.Status = KindFriends
	case StatusPending:
		if req.RequesterID == viewerID {
			rs.Status = KindPendingSent
		} else {
			rs.Status = KindPendingReceived
		}
	case StatusDeclined:
		fallthrough
	default:
		rs.Status = KindNone
		rs.RequestID = nil
	}

	return rs, nil
}
