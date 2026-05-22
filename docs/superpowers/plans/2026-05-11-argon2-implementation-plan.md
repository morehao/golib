# Argon2id Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 gcrypto 添加 Argon2id 密码哈希支持，提供与 bcrypt 一致的 API 风格

**Architecture:** 独立的 argon2.go 文件，实现两个导出函数，使用 golang.org/x/crypto/argon2 库

**Tech Stack:** Go, golang.org/x/crypto/argon2

---

## 文件结构

- 创建: `gcrypto/argon2.go` — Argon2id 核心实现
- 创建: `gcrypto/argon2_test.go` — 测试文件

---

## Task 1: 实现 argon2.go

**Files:**
- 创建: `gcrypto/argon2.go`

- [ ] **Step 1: 编写 GenerateArgon2idHash 函数**

```go
package gcrypto

import (
	"crypto/rand"
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
```

- [ ] **Step 2: 编写 CompareArgon2idHash 函数**

```go
func CompareArgon2idHash(hashedPassword, password string) error {
	parts := strings.Split(hashedPassword, "$")
	if len(parts) != 6 {
		return fmt.Errorf("invalid hash format")
	}

	if parts[0] != "$argon2id" || parts[1] != "v=19" {
		return fmt.Errorf("unsupported argon2id version")
	}

	var m, t, p int
	_, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &m, &t, &p)
	if err != nil {
		return fmt.Errorf("invalid parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return fmt.Errorf("invalid salt: %w", err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("invalid hash: %w", err)
	}

	actualHash := argon2.IDKey([]byte(password), salt, uint32(t), uint32(m), uint32(p), uint32(len(expectedHash)))

	if subtle.ConstantTimeCompare(expectedHash, actualHash) != 1 {
		return fmt.Errorf("password mismatch")
	}

	return nil
}
```

- [ ] **Step 3: 添加 crypto/subtle import**

在文件顶部添加 `"crypto/subtle"` import

- [ ] **Step 4: 提交argon2.go**

---

## Task 2: 实现 argon2_test.go

**Files:**
- 创建: `gcrypto/argon2_test.go`

- [ ] **Step 1: 编写测试用例**

```go
package gcrypto

import (
	"testing"
)

func TestGenerateArgon2idHash(t *testing.T) {
	hash, err := GenerateArgon2idHash("password")
	if err != nil {
		t.Fatalf("GenerateArgon2idHash failed: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=1,p=4$") {
		t.Errorf("unexpected hash format: %s", hash)
	}
}

func TestCompareArgon2idHash_Success(t *testing.T) {
	hash, err := GenerateArgon2idHash("password")
	if err != nil {
		t.Fatalf("GenerateArgon2idHash failed: %v", err)
	}

	err = CompareArgon2idHash(hash, "password")
	if err != nil {
		t.Errorf("CompareArgon2idHash should succeed for correct password: %v", err)
	}
}

func TestCompareArgon2idHash_WrongPassword(t *testing.T) {
	hash, err := GenerateArgon2idHash("password")
	if err != nil {
		t.Fatalf("GenerateArgon2idHash failed: %v", err)
	}

	err = CompareArgon2idHash(hash, "wrongpassword")
	if err == nil {
		t.Errorf("CompareArgon2idHash should fail for wrong password")
	}
}

func TestCompareArgon2idHash_EmptyPassword(t *testing.T) {
	hash, err := GenerateArgon2idHash("password")
	if err != nil {
		t.Fatalf("GenerateArgon2idHash failed: %v", err)
	}

	err = CompareArgon2idHash(hash, "")
	if err == nil {
		t.Errorf("CompareArgon2idHash should fail for empty password")
	}
}

func TestGenerateArgon2idHash_SpecialChars(t *testing.T) {
	passwords := []string{"密码", "🔐🔑", "!@#$%^&*()"}
	for _, pwd := range passwords {
		hash, err := GenerateArgon2idHash(pwd)
		if err != nil {
			t.Fatalf("GenerateArgon2idHash failed for %s: %v", pwd, err)
		}
		err = CompareArgon2idHash(hash, pwd)
		if err != nil {
			t.Errorf("CompareArgon2idHash failed for %s: %v", pwd, err)
		}
	}
}
```

- [ ] **Step 2: 运行测试验证**

Run: `go test -v ./gcrypto -run Argon2`
Expected: 所有测试 PASS

- [ ] **Step 3: 提交argon2_test.go**

---

**Plan complete. Two execution options:**

1. **Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?