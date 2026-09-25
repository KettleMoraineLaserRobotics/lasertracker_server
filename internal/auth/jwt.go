package auth

import (
	"context"
	"errors"
	"time"

	"lasertracker_server/internal"
	actions "lasertracker_server/internal/actions"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	Username string
	GroupKey string
	TokenVer int
	jwt.RegisteredClaims
}

func GenerateToken(username string, groupKey string, tokenVer int, secret string) (string, error) {
	expirationTime := time.Now().Add(96 * time.Hour)

	claims := &Claims{
		Username: username,
		GroupKey: groupKey,
		TokenVer: tokenVer,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateToken(tokenString string, ctx context.Context) (*Claims, error) {
	claims := &Claims{}
	secret := internal.GetConfig().JWTSecret
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("Unexpected signing method")
		}
		return []byte(secret), nil
	})

	if err != nil || !token.Valid {
		return nil, errors.New("Unauthorized: Invalid token")
	}

	currentVer, err := actions.GetMemberTokenVer(ctx, claims.GroupKey, claims.Username)
	if err != nil || claims.TokenVer != currentVer {
		return nil, errors.New("Unauthorized: Session expired, please log in again")
	}

	return claims, nil
}
