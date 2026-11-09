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
	if err != nil {
		return err
	}

	for _, match := range matches {
		if err := actions.AddMatch(ctx, match); err != nil {
			log.Printf("Error adding match %s to table: %v", match.MatchKey, err)
		}
	}

	return nil
}

func RegisterEventInBackground(eventKey string) {
	go func() {
		if err := RegisterEvent(context.Background(), eventKey); err != nil {
			log.Printf("Error registering event %s: %v", eventKey, err)
		}
	}()
}
