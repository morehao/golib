package golib

import (
	"os"
	"testing"

	"github.com/morehao/golib/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.Load()
	os.Exit(m.Run())
}