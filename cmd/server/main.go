package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"lasertracker_server/internal"
	"lasertracker_server/internal/actions"
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

	log.Printf("%s", "Starting "+cfg.InstanceName+" on port "+strconv.Itoa(cfg.Port))
	if err := http.InitHTTPServer(ctx, hub); err != nil {
		log.Fatalf("HTTP server stopped: %v", err)
	}
}
