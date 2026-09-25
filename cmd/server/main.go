package main

import (
	"log"
	"strconv"

	"lasertracker_server/internal"
	httpserver "lasertracker_server/internal/http"
)

func main() {
	cfg := internal.GetConfig()

	httpserver.InitHTTPServer()
	log.Printf("%s", "Starting "+cfg.InstanceName+" on port "+strconv.Itoa(cfg.Port))

}
