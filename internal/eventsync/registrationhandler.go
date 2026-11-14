package eventsync

import (
	"context"
	actions "lasertracker_server/internal/actions"
	"log"
)

func RegisterEvent(ctx context.Context, eventKey string) error {
	if err := actions.EnsureTrackedEvent(ctx, eventKey); err != nil {
		return err
	}
	Poke(eventKey)
	return nil
}

func RegisterEventInBackground(eventKey string) {
	go func() {
		if err := RegisterEvent(context.Background(), eventKey); err != nil {
			log.Printf("Error registering event %s: %v", eventKey, err)
		}
	}()
}
