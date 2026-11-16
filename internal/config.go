package internal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
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
	TBAAPIURL    string
	JWTSecret    string
	TrustProxy   bool
}

func loadConfig() *config {
	instName := os.Getenv("LT_INSTANCE_NAME")
	ver := os.Getenv("LT_VERSION")
	dbUrl := os.Getenv("LT_DATABASE_URL")
	port, err := strconv.Atoi(os.Getenv("LT_PORT"))
	if err != nil || port <= 0 {
		port = 2077
	}
	tba := os.Getenv("LT_TBA_API_KEY")
	tbaURL := strings.TrimRight(os.Getenv("LT_TBA_API_URL"), "/")
	if tbaURL == "" {
		tbaURL = "https://www.thebluealliance.com/api/v3"
	}
	trustProxy := os.Getenv("LT_TRUST_PROXY") == "true"
	jwt := loadJWTSecret()

	var cfg config
	cfg.InstanceName = instName
	cfg.Version = ver
	cfg.DatabaseURL = dbUrl
	cfg.Port = port
	cfg.TBAAPIKey = tba
	cfg.TBAAPIURL = tbaURL
	cfg.JWTSecret = jwt
	cfg.TrustProxy = trustProxy

	return &cfg
}

func loadJWTSecret() string {
	secretBytes, err := os.ReadFile(".secret")
	if err == nil {
		if secret := strings.TrimSpace(string(secretBytes)); secret != "" {
			return secret
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Unable to read .secret: %v", err)
	}

	secret, err := GenerateRandomSecret(32)
	if err != nil {
		log.Fatalf("Unable to generate JWT secret: %v", err)
	}
	if err := os.WriteFile(".secret", []byte(secret+"\n"), 0600); err != nil {
		log.Fatalf("Unable to save JWT secret to .secret: %v", err)
	}
	log.Printf("Generated a new JWT secret and saved it to .secret")

	return secret
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
