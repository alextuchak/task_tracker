package persistence

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateUTF8(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"short ascii is untouched", "boom", 10, "boom"},
		{"exactly at the limit", "boom", 4, "boom"},
		{"cut lands between runes", "日本語", 6, "日本"},
		{"cut lands inside a rune", "日本語", 7, "日本"},
		{"limit shorter than the first rune", "日", 2, ""},
		{"zero limit", "日", 0, ""},
		{"already invalid input under the limit", "ok\xff", 10, "ok"},
		{"already invalid input over the limit", "\xffab", 2, "a"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncateUTF8(c.in, c.limit)

			assert.Equal(t, c.want, got)
			assert.True(t, utf8.ValidString(got), "the column is utf8mb4 and rejects invalid bytes")
			assert.LessOrEqual(t, len(got), max(c.limit, len(c.in)))
		})
	}
}

func TestTruncateUTF8KeepsAsMuchAsTheLimitAllows(t *testing.T) {
	long := strings.Repeat("日", 700)

	got := truncateUTF8(long, maxErrLen)

	require.True(t, utf8.ValidString(got))
	assert.True(t, strings.HasPrefix(long, got), "the kept part must be a prefix of the original")
	assert.Greater(t, len(got), maxErrLen-utf8.UTFMax, "truncation must not back off further than one rune")
}
