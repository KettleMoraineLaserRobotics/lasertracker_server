package ws

import (
	"errors"
	"lasertracker_server/internal/actions"
	"log"
)

type wsError struct{ Code, Message string }

func (e *wsError) Error() string { return e.Code + ": " + e.Message }

var (
	errForbidden = &wsError{"forbidden", "you don't have permission to do that"}
)

func errInvalidPayload(err error) error {
	log.Printf("invalid payload: %v", err)
	return &wsError{"invalid_payload", "payload could not be parsed"}
}

func toWSError(err error) *wsError {
	var we *wsError
	switch {
	case errors.As(err, &we):
		return we
	case errors.Is(err, actions.ErrNotFound):
		return &wsError{"not_found", "not found"}
	case errors.Is(err, actions.ErrAlreadyExists):
		return &wsError{"already_exists", "already exists"}
	case errors.Is(err, actions.ErrForeignKeyFailed):
		return &wsError{"invalid_payload", "references something that doesn't exist"}
	default:
		log.Printf("internal error: %v", err)
		return &wsError{"internal", "something went wrong"}
	}
}
