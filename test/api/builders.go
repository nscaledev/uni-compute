package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func generateRandomName(prefix string) string {
	bytes := make([]byte, 4) // 8 hex characters
	rand.Read(bytes)
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(bytes))
}

func GenerateTestID() string {
	return generateRandomName("test")
}
