package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDialectorName(t *testing.T) {
	d := &dialector{}
	assert.Equal(t, "postgres", d.Name())
}

func TestDialectorMatchURL(t *testing.T) {
	d := &dialector{}
	assert.True(t, d.MatchURL("postgres://postgres:123456@127.0.0.1:5432/demo"))
	assert.True(t, d.MatchURL("postgresql://postgres:123456@127.0.0.1:5432/demo"))
	assert.True(t, d.MatchURL("POSTGRES://postgres:123456@127.0.0.1:5432/demo"))
	assert.False(t, d.MatchURL("mysql://root:123456@127.0.0.1:3306/demo"))
}

func TestDialectorParseURL(t *testing.T) {
	d := &dialector{}
	db, err := d.ParseURL("postgres://postgres:123456@127.0.0.1:5432/demo?sslmode=disable")
	assert.Nil(t, err)
	assert.Equal(t, "demo", db)

	db, err = d.ParseURL("postgresql://postgres:123456@127.0.0.1:5432/demo?sslmode=disable")
	assert.Nil(t, err)
	assert.Equal(t, "demo", db)
}
