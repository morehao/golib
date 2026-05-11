package gcrypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2idSaltLen = 16
	argon2idKeyLen  = 32
	argon2idM       = 65536
	argon2idT       = 1
	argon2idP       = 4
)

func GenerateArgon2idHash(password string) (string, error) {
	salt := make([]byte, argon2idSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argon2idT, argon2idM, argon2idP, argon2idKeyLen)

	saltB64 := base64.RawStdEncoding.EncodeToString(salt)
	hashB64 := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argon2idM, argon2idT, argon2idP, saltB64, hashB64), nil
}

func CompareArgon2idHash(hashedPassword, password string) error {
	parts := strings.Split(hashedPassword, "$")
	if len(parts) != 6 {
		return fmt.Errorf("invalid hash format")
	}

	if parts[1] != "argon2id" || parts[2] != "v=19" {
		return fmt.Errorf("unsupported argon2id version")
	}

	var m, t, p int
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p)
	if err != nil {
		return fmt.Errorf("invalid parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("invalid salt: %w", err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("invalid hash: %w", err)
	}

	actualHash := argon2.IDKey([]byte(password), salt, uint32(t), argon2idM, uint8(p), uint32(len(expectedHash)))

	if subtle.ConstantTimeCompare(expectedHash, actualHash) != 1 {
		return fmt.Errorf("password mismatch")
	}

	return nil
}