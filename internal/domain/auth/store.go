package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const refreshKeyPrefix = "refresh:"

// RefreshTokenStore persists refresh-token-hash → user-id mappings in Redis.
// The raw token is never stored — only the SHA-256 hash supplied by the caller.
type RefreshTokenStore struct {
	client *redis.Client
}

func NewRefreshTokenStore(client *redis.Client) *RefreshTokenStore {
	return &RefreshTokenStore{client: client}
}

// Store sets the hash → userID mapping with the given TTL.
func (s *RefreshTokenStore) Store(ctx context.Context, userID uuid.UUID, hash []byte, ttl time.Duration) error {
	if err := s.client.Set(ctx, redisKey(hash), userID.String(), ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

// Consume atomically reads and deletes the entry for the given hash, returning
// the owning user id. Missing or already-consumed tokens yield ErrRefreshTokenInvalid.
func (s *RefreshTokenStore) Consume(ctx context.Context, hash []byte) (uuid.UUID, error) {
	val, err := s.client.GetDel(ctx, redisKey(hash)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return uuid.Nil, ErrRefreshTokenInvalid
		}
		return uuid.Nil, fmt.Errorf("redis getdel: %w", err)
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse user id: %w", err)
	}
	return id, nil
}

// Revoke deletes the entry for the given hash. Missing entries are not an error.
func (s *RefreshTokenStore) Revoke(ctx context.Context, hash []byte) error {
	if err := s.client.Del(ctx, redisKey(hash)).Err(); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	return nil
}

func redisKey(hash []byte) string {
	return refreshKeyPrefix + hex.EncodeToString(hash)
}
