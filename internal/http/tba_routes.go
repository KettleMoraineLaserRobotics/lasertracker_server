package http

import (
	"encoding/json"
	"errors"
	"lasertracker_server/internal/actions"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const tbaCacheTTL = time.Hour
const tbaCacheMax = 500

type cacheEntry struct {
	body    []byte
	expires time.Time
}

type responseCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

func newResponseCache() *responseCache {
	return &responseCache{entries: make(map[string]cacheEntry)}
}

func (c *responseCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return e.body, true
}

func (c *responseCache) set(key string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= tbaCacheMax {
		c.entries = make(map[string]cacheEntry)
	}
	c.entries[key] = cacheEntry{body: body, expires: time.Now().Add(tbaCacheTTL)}
}

func parseTeamNumber(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || n < 1 || n > 99999 {
		http.Error(w, "Invalid team number", http.StatusBadRequest)
		return 0, false
	}
	return n, true
}

func tbaError(w http.ResponseWriter, err error) {
	if errors.Is(err, actions.ErrTBANotFound) {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}
	http.Error(w, "Could not reach The Blue Alliance", http.StatusBadGateway)
}

func writeCached(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func teamInfoHandler(cache *responseCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		team, ok := parseTeamNumber(w, r)
		if !ok {
			return
		}

		key := "info/" + strconv.Itoa(team)
		if body, hit := cache.get(key); hit {
			writeCached(w, body)
			return
		}

		name, err := actions.TeamName(team)
		if err != nil {
			tbaError(w, err)
			return
		}
		avatar, _ := actions.TeamAvatar(team)

		body, err := json.Marshal(map[string]string{"team_name": name, "team_avatar_b64": avatar})
		if err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
		cache.set(key, body)
		writeCached(w, body)
	}
}

func teamEventsHandler(cache *responseCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		team, ok := parseTeamNumber(w, r)
		if !ok {
			return
		}

		key := "events/" + strconv.Itoa(team)
		if body, hit := cache.get(key); hit {
			writeCached(w, body)
			return
		}

		events, err := actions.GetTeamEvents(team)
		if err != nil {
			tbaError(w, err)
			return
		}
		if events == nil {
			events = []map[string]string{}
		}

		body, err := json.Marshal(events)
		if err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
		cache.set(key, body)
		writeCached(w, body)
	}
}
