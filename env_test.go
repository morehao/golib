package golib

import (
	"os"
	"testing"

	"github.com/morehao/golib/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.Load()
	os.Exit(m.Run())
}