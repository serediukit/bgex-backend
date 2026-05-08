package friends

import "errors"

var (
	ErrNotFound      = errors.New("friend request not found")
	ErrAlreadyExists = errors.New("friend relationship already exists")
	ErrSelfFriend    = errors.New("cannot send friend request to yourself")
	ErrForbidden     = errors.New("not authorized to modify this request")
)
