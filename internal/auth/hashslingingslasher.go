package auth

// the slash-bringing hasher

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func HashPin(pin string) string {
	hashword, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		fmt.Println(err)
	}
	return string(hashword)
}

func CheckPin(hashed string, unhashed string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(unhashed))
	return err == nil
}
