package testenv

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

func Load() {
	dir, _ := os.Getwd()
	for {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}