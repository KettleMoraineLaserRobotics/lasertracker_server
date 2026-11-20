package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"lasertracker_server/internal"
	"lasertracker_server/internal/actions"
	"lasertracker_server/internal/eventsync"
	"lasertracker_server/internal/http"
	"lasertracker_server/internal/ws"
)

func main() {
	cfg := internal.GetConfig()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := actions.CreateTables(); err != nil {
		log.Fatalf("Failed to set up database tables: %v", err)
	}

	hub := ws.NewHub()
	go hub.Run(ctx)
	go eventsync.Start(ctx, hub)

	actions.OnLogAdded = func(entry internal.LogEntry) {
		payload, err := json.Marshal(entry)
		if err != nil {
			return
		}
		hub.Broadcast(internal.Message{GroupKey: entry.GroupKey, Timestamp: time.Now(), InfoType: "LOG_ADDED", Payload: payload})
	}

	log.Printf("%s", "Starting "+cfg.InstanceName+" on port "+strconv.Itoa(cfg.Port))
	if err := http.InitHTTPServer(ctx, hub); err != nil {
		log.Fatalf("HTTP server stopped: %v", err)
	}
}
