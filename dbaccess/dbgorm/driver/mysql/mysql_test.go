package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeMySQLURI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		hasError bool
	}{
		{
			name:     "standard uri with port and params",
			input:    "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4&parseTime=true",
			expected: "root:123456@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=true",
		},
		{
			name:     "uri with default port",
			input:    "mysql://root:123456@127.0.0.1/demo",
			expected: "root:123456@tcp(127.0.0.1:3306)/demo",
		},
		{
			name:     "uri without password",
			input:    "mysql://root@127.0.0.1/demo",
			expected: "root:@tcp(127.0.0.1:3306)/demo",
		},
		{
			name:     "uri with special chars password",
			input:    "mysql://root:p@ssw0rd@127.0.0.1:3307/demo",
			expected: "root:p@ssw0rd@tcp(127.0.0.1:3307)/demo",
		},
		{
			name:     "uri without database",
			input:    "mysql://root:123456@127.0.0.1:3306/",
			expected: "root:123456@tcp(127.0.0.1:3306)/",
		},
		{
			name:     "uri with only query params",
			input:    "mysql://root:123456@127.0.0.1:3307/demo?timeout=5s&readTimeout=3s",
			expected: "root:123456@tcp(127.0.0.1:3307)/demo?timeout=5s&readTimeout=3s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizeMySQLURI(tt.input)
			if tt.hasError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDialectorName(t *testing.T) {
	d := &dialector{}
	assert.Equal(t, "mysql", d.Name())
}

func TestDialectorMatchURL(t *testing.T) {
	d := &dialector{}
	assert.True(t, d.MatchURL("mysql://root:123456@127.0.0.1:3306/demo"))
	assert.True(t, d.MatchURL("MYSQL://root:123456@127.0.0.1:3306/demo"))
	assert.False(t, d.MatchURL("postgres://postgres:123456@127.0.0.1:5432/demo"))
}

func TestDialectorParseURL(t *testing.T) {
	d := &dialector{}
	db, err := d.ParseURL("mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4")
	assert.Nil(t, err)
	assert.Equal(t, "demo", db)
}
