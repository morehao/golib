package gcrypto

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// GeneratePasswordHash 使用默认成本生成密码哈希
func GeneratePasswordHash(password string) (string, error) {
	return GeneratePasswordHashWithCost(password, bcrypt.DefaultCost)
}

// GeneratePasswordHashWithCost 使用指定成本生成密码哈希
func GeneratePasswordHashWithCost(password string, cost int) (string, error) {
	if password == "" {
		return "", errors.New("password is empty")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// ComparePasswordHash 校验密码是否与哈希匹配
func ComparePasswordHash(hashedPassword, password string) error {
	if hashedPassword == "" {
		return errors.New("hashed password is empty")
	}
	if password == "" {
		return errors.New("password is empty")
	}

	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
