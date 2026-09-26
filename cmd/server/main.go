package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"lasertracker_server/internal"
	"lasertracker_server/internal/http"
	"lasertracker_server/internal/ws"
)

func main() {
	cfg := internal.GetConfig()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	hub := ws.NewHub()
	go hub.Run(ctx)

	http.InitHTTPServer(ctx, hub)
	log.Printf("%s", "Starting "+cfg.InstanceName+" on port "+strconv.Itoa(cfg.Port))
}
