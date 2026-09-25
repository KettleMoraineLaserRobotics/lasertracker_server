package internal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

type config struct {
	InstanceName string
	Version      string
	DatabaseURL  string
	Port         int
	TBAAPIKey    string
	JWTSecret    string
}

func loadConfig() *config {
	instName := os.Getenv("LT_INSTANCE_NAME")
	ver := os.Getenv("LT_VERSTION")
	dbUrl := os.Getenv("LT_DATABASE_URL")
	port, _ := strconv.Atoi(os.Getenv("LT_PORT"))
	tba := os.Getenv(" LT_TBA_API_KEY")
	jwt := "Hi, I am a secret. My background consists of letters and numbers."
	if _, err := os.Stat(".secret"); err == nil {
		secretBytes, readErr := os.ReadFile(".secret")
		if readErr == nil {
			jwt = strings.TrimSpace(string(secretBytes))
		}
	} else {
		secret, _ := GenerateRandomSecret(32)
		jwt = secret
	}

	var cfg config
	cfg.InstanceName = instName
	cfg.Version = ver
	cfg.DatabaseURL = dbUrl
	cfg.Port = port
	cfg.TBAAPIKey = tba
	cfg.JWTSecret = jwt

	return &cfg
}

var lock = &sync.Mutex{}

var cfgInstance *config

func GetConfig() *config {
	if cfgInstance == nil {
		lock.Lock()
		defer lock.Unlock()
		if cfgInstance == nil {
			cfgInstance = loadConfig()
		}
	}

	return cfgInstance
}

func GenerateRandomSecret(length int) (string, error) {
	bytes := make([]byte, length)

	_, err := rand.Read(bytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}

	return hex.EncodeToString(bytes), nil
}
