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
	JWTSecret    string
	IsTesting    bool
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
	testingStr := os.Getenv("LT_IS_TESTING")
	testingBool := false
	if testingStr == "true" {
		testingBool = true
	}
	jwt := loadJWTSecret()

	var cfg config
	cfg.InstanceName = instName
	cfg.Version = ver
	cfg.DatabaseURL = dbUrl
	cfg.Port = port
	cfg.TBAAPIKey = tba
	cfg.JWTSecret = jwt
	cfg.IsTesting = testingBool

	return &cfg
}

func loadJWTSecret() string {
	if envSecret := strings.TrimSpace(os.Getenv("LT_JWT_SECRET")); envSecret != "" {
		return envSecret
	}

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
