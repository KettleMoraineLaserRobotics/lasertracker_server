package eventsync

import (
	"context"
	actions "lasertracker_server/internal/actions"
	"log"
)

func RegisterEvent(ctx context.Context, eventKey string) error {
	tracked, err := actions.IsEventTracked(ctx, eventKey)
	if err != nil {
		return err
	}

	if tracked {
		return nil
	}

	matches, err := actions.GetEventMatches(eventKey)

	for match := range matches {
		err := actions.AddMatch(ctx, matches[match])
		if err != nil {
			log.Printf("Error adding match to table: ", err)
		}
	}
}
