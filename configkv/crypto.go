package configkv

import (
	"errors"
	"strings"

	"github.com/morehao/golib/gcrypto"
)

const (
	defaultCryptoKey = "SASItKkEmhTtfAKAr1+8N0Oq2tP2+c6LW0GQ7ovlFJs="
	encryptedPrefix  = "enc:"
)

var errInvalidKey = errors.New("invalid key")

type aesCrypto struct {
	aes *gcrypto.AES
}

func newAESCrypto(key []byte) (*aesCrypto, error) {
	aes, err := gcrypto.NewAES(string(key))
	if err != nil {
		return nil, err
	}
	return &aesCrypto{aes: aes}, nil
}

func (c *aesCrypto) Encrypt(plaintext string) (string, error) {
	ciphertext, err := c.aes.EncryptString(plaintext)
	if err != nil {
		return "", err
	}
	return encryptedPrefix + ciphertext, nil
}

func (c *aesCrypto) Decrypt(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, encryptedPrefix) {
		return "", errors.New("invalid ciphertext format")
	}
	ct := strings.TrimPrefix(ciphertext, encryptedPrefix)
	return c.aes.DecryptString(ct)
}
