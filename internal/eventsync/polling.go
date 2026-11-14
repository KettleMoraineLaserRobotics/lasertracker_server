package eventsync

import (
	"context"
	"encoding/json"
	"errors"
	"lasertracker_server/internal"
	actions "lasertracker_server/internal/actions"
	"log"
	"math/rand"
	"sync"
	"time"
)

type Broadcaster interface{ Broadcast(internal.Message) }

var pokeCh = make(chan string, 64)

func Poke(eventKey string) {
	select {
	case pokeCh <- eventKey:
	default:
	}
}

func Start(ctx context.Context, b Broadcaster) {
	ticker := time.NewTicker(5 * time.Second)
	sem := make(chan struct{}, 4)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			actions.EnsureTrackedEventsForGroups(ctx)
			due, _ := actions.ClaimDueEvents(ctx, 16)
			for _, ev := range due {
				dispatch(ctx, ev, sem, b)
			}
		case key := <-pokeCh:
			actions.EnsureTrackedEvent(ctx, key)
			actions.MarkDueNow(ctx, key)
		}
	}
}

var (
	infoFetched   = map[string]time.Time{}
	infoFetchedMu sync.Mutex
)

func infoIsStale(eventKey string) bool {
	infoFetchedMu.Lock()
	defer infoFetchedMu.Unlock()
	t, ok := infoFetched[eventKey]
	return !ok || time.Since(t) > time.Hour
}

func markInfoFetched(eventKey string) {
	infoFetchedMu.Lock()
	defer infoFetchedMu.Unlock()
	infoFetched[eventKey] = time.Now()
}

func dispatch(ctx context.Context, ev internal.TrackedEvent, sem chan struct{}, b Broadcaster) {
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	go func() {
		defer func() { <-sem }()
		pollOne(ctx, ev, b)
	}()
}

func pollOne(ctx context.Context, ev internal.TrackedEvent, b Broadcaster) {
	start, end := ev.StartDate, ev.EndDate

	if start == nil || end == nil || infoIsStale(ev.EventKey) {
		s, e, webcasts, err := actions.FetchEventInfo(ctx, ev.EventKey)
		if err != nil {
			failPoll(ctx, ev, err)
			return
		}
		if err := actions.SaveEventInfo(ctx, ev.EventKey, s, e, webcasts); err != nil {
			log.Printf("Could not save event info for %s: %v", ev.EventKey, err)
		}
		markInfoFetched(ev.EventKey)
		start, end = s, e
	}

	matches, newETag, maxAge, notModified, err := actions.FetchEventMatches(ctx, ev.EventKey, ev.ETag)
	if err != nil {
		failPoll(ctx, ev, err)
		return
	}

	next := nextPoll(time.Now(), start, end, maxAge)

	if notModified {
		recordPoll(ctx, ev.EventKey, "", false, next, 0)
		return
	}

	changedKeys, err := actions.UpsertMatches(ctx, matches)
	if err != nil {
		failPoll(ctx, ev, err)
		return
	}
	if len(changedKeys) > 0 {
		broadcastMatches(ctx, b, ev.EventKey)
	}

	recordPoll(ctx, ev.EventKey, newETag, len(changedKeys) > 0, next, 0)
}

func recordPoll(ctx context.Context, eventKey, etag string, changed bool, next time.Time, failCount int) {
	if err := actions.RecordPoll(ctx, eventKey, etag, changed, next, failCount); err != nil {
		log.Printf("Could not record poll for %s: %v", eventKey, err)
	}
}

func failPoll(ctx context.Context, ev internal.TrackedEvent, err error) {
	failCount := ev.FailCount
	var next time.Time
	switch {
	case errors.Is(err, actions.ErrTBAAuth):
		log.Printf("TBA rejected the API key while polling %s", ev.EventKey)
		next = time.Now().Add(15 * time.Minute)
	case errors.Is(err, actions.ErrTBANotFound):
		log.Printf("TBA has no data for %s, trying again in 6h", ev.EventKey)
		next = time.Now().Add(6 * time.Hour)
	default:
		failCount++
		log.Printf("Polling %s failed (%d in a row): %v", ev.EventKey, failCount, err)
		d := min(15*time.Minute, 30*time.Second<<min(failCount, 5))
		next = time.Now().Add(d + time.Duration(rand.Int63n(int64(d)/5+1)))
	}
	recordPoll(ctx, ev.EventKey, "", false, next, failCount)
}

func nextPoll(now time.Time, start, end *time.Time, maxAge time.Duration) time.Time {
	if start == nil || end == nil {
		return now.Add(15 * time.Minute)
	}

	endOfEvent := end.Add(24 * time.Hour)
	var tier time.Duration
	switch {
	case !now.Before(*start) && now.Before(endOfEvent):
		tier = time.Second * 30
	case now.Before(*start):
		until := start.Sub(now)
		switch {
		case until <= 24*time.Hour:
			tier = 2 * time.Minute
		case until <= 7*24*time.Hour:
			tier = 15 * time.Minute
		default:
			tier = 6 * time.Hour
		}
	default:
		since := now.Sub(endOfEvent)
		switch {
		case since <= 3*24*time.Hour:
			tier = 10 * time.Minute
		case since <= 14*24*time.Hour:
			tier = 6 * time.Hour
		default:
			return now.AddDate(10, 0, 0)
		}
	}

	return now.Add(max(tier, maxAge))
}

func broadcastMatches(ctx context.Context, b Broadcaster, eventKey string) {
	groups, err := actions.GroupsAtEvent(ctx, eventKey)
	if err != nil {
		log.Printf("Could not list groups at %s: %v", eventKey, err)
		return
	}
	for _, g := range groups {
		ms, err := actions.GetTeamMatches(ctx, g.TeamNumber, eventKey)
		if err != nil {
			log.Printf("Could not load matches for group %s: %v", g.GroupKey, err)
			continue
		}
		payload, _ := json.Marshal(map[string]any{"event_key": eventKey, "matches": ms})
		b.Broadcast(internal.Message{GroupKey: g.GroupKey, Timestamp: time.Now(), InfoType: "MATCHES_UPDATED", Payload: payload})
	}
}
