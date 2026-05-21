package storage

import (
	"crypto/rand"
	"encoding/hex"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type KeyBuilder struct {
	prefix       string
	dateLayout   string
	randomSuffix bool
	preserveExt  bool
	now          func() time.Time
}

func NewKeyBuilder() *KeyBuilder {
	return &KeyBuilder{now: time.Now}
}

func (b *KeyBuilder) WithPrefix(v string) *KeyBuilder         { b.prefix = strings.Trim(v, "/"); return b }
func (b *KeyBuilder) WithDateLayout(v string) *KeyBuilder      { b.dateLayout = v; return b }
func (b *KeyBuilder) WithRandomSuffix() *KeyBuilder            { b.randomSuffix = true; return b }
func (b *KeyBuilder) PreserveExt() *KeyBuilder                 { b.preserveExt = true; return b }
func (b *KeyBuilder) WithNow(fn func() time.Time) *KeyBuilder  { b.now = fn; return b }

func (b *KeyBuilder) Build(name string) string {
	clean := sanitizeFileName(name)
	ext := ""
	base := clean
	if b.preserveExt {
		ext = filepath.Ext(clean)
		base = strings.TrimSuffix(clean, ext)
	}
	if b.randomSuffix {
		base += "_" + randomHex(4)
	}
	parts := make([]string, 0, 3)
	if b.prefix != "" {
		parts = append(parts, b.prefix)
	}
	if b.dateLayout != "" {
		parts = append(parts, b.now().Format(b.dateLayout))
	}
	parts = append(parts, base+ext)
	return path.Join(parts...)
}

func sanitizeFileName(v string) string {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(v)))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	return strings.TrimLeft(name, ".-")
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
