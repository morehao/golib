package gutil

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// GenUUID 生成一个全局唯一的 UUID(v7) 字符串，常用于请求 ID、任务运行 ID 等。
func GenUUID() string {
	return uuid.Must(uuid.NewV7()).String()
}

func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

func RandomHex(n int) (string, error) {
	b, err := RandomBytes(n)
	return hex.EncodeToString(b), err
}

func RandomBase64(n int) (string, error) {
	b, err := RandomBytes(n)
	return base64.StdEncoding.EncodeToString(b), err
}

func RandomString(length int) (string, error) {
	b, err := RandomBytes((length + 1) / 2)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b)[:length], nil
}

func RandomDigits(length int) (string, error) {
	b, err := RandomBytes(length)
	if err != nil {
		return "", fmt.Errorf("random digits: %w", err)
	}
	digits := make([]byte, length)
	for i, v := range b {
		digits[i] = '0' + v%10
	}
	return string(digits), nil
}

// RandomFromSet 从给定的字符集 set 中等概率生成一个长度为 length 的随机字符串。
// 用于需要限定字符范围（如仅字母、仅可打印符号、自定义排除易混淆字符）的场景。
//
// set 必须非空；length < 0 视为 0。随机源为 crypto/rand，模偏差可忽略
// （字符集长度通常远小于 256，对非密码学标识场景足够；如用于口令/密钥请评估）。
//
// 常见字符集常量见 RandomCharset* 定义。
func RandomFromSet(length int, set string) (string, error) {
	if set == "" {
		return "", fmt.Errorf("random from set: empty charset")
	}
	if length <= 0 {
		return "", nil
	}
	out := make([]byte, length)
	n := len(set)
	for i := 0; i < length; i++ {
		v, err := randInt(n)
		if err != nil {
			return "", fmt.Errorf("random from set: %w", err)
		}
		out[i] = set[v]
	}
	return string(out), nil
}

// 常用字符集，供 RandomFromSet 使用。
const (
	// RandomCharsetNumber 数字 0-9。
	RandomCharsetNumber = "0123456789"
	// RandomCharsetAlpha 大小写字母。
	RandomCharsetAlpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// RandomCharsetAlphanumeric 数字 + 大小写字母。
	RandomCharsetAlphanumeric = RandomCharsetNumber + RandomCharsetAlpha
	// RandomCharsetSymbol 数字 + 大小写字母 + 常见可打印符号。
	RandomCharsetSymbol = RandomCharsetAlphanumeric + "~!@#$%^&*_+?-="
)

// randInt 用 crypto/rand 返回 [0, n) 的均匀随机整数。
func randInt(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("invalid bound: %d", n)
	}
	if n <= 256 {
		var b [1]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		return int(b[0]) % n, nil
	}
	// n > 256：取足够字节拼成整数后取模
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, err
	}
	return int(uint32(buf[0])<<24|uint32(buf[1])<<16|uint32(buf[2])<<8|uint32(buf[3])) % n, nil
}
