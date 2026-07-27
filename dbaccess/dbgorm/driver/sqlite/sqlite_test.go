package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDialectorName(t *testing.T) {
	d := &dialector{}
	assert.Equal(t, "sqlite", d.Name())
}

func TestDialectorMatchURL(t *testing.T) {
	d := &dialector{}
	assert.True(t, d.MatchURL("sqlite://:memory:"))
	assert.True(t, d.MatchURL("sqlite:///path/to/db.sqlite"))
	assert.False(t, d.MatchURL("mysql://root:123456@127.0.0.1:3306/demo"))
}

func TestDialectorParseURL(t *testing.T) {
	d := &dialector{}
	db, err := d.ParseURL("sqlite://:memory:")
	assert.Nil(t, err)
	assert.Equal(t, ":memory:", db)

	db, err = d.ParseURL("sqlite:///path/to/db.sqlite")
	assert.Nil(t, err)
	assert.Equal(t, "/path/to/db.sqlite", db)
}
