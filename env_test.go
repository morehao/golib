package golib

import (
	"os"
	"testing"

	"github.com/morehao/golib/internal/testkit"
)

func TestMain(m *testing.M) {
	testkit.Load()
	os.Exit(m.Run())
}