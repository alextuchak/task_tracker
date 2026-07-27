package outbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShardByCoversAllOnce(t *testing.T) {
	items := make([]Claimed, 10)
	for i := range items {
		items[i].ID = int64(i)
	}

	shards := shardBy(items, 4)
	require.LessOrEqual(t, len(shards), 4)

	seen := make([]int64, 0, len(items))
	for _, s := range shards {
		seen = append(seen, idsOf(s)...)
	}
	assert.ElementsMatch(t, idsOf(items), seen)
}

func TestShardByFewerItemsThanWorkers(t *testing.T) {
	items := make([]Claimed, 2)
	shards := shardBy(items, 5)
	assert.Len(t, shards, 2) // one item per shard, no empty shards
}

func TestShardByEmpty(t *testing.T) {
	assert.Empty(t, shardBy(nil, 4))
}

func TestBackoffFirstAttemptAroundOneSecond(t *testing.T) {
	d := backoff(1)
	assert.Greater(t, d, 800*time.Millisecond)
	assert.Less(t, d, 1200*time.Millisecond)
}

func TestBackoffCapped(t *testing.T) {
	// attempt^4 would be enormous; the cap plus max +10% jitter bounds it
	assert.LessOrEqual(t, backoff(1000), backoffCap*110/100)
}

// idsOf flattens shards for the coverage assertions below.
func idsOf(items []Claimed) []int64 {
	ids := make([]int64, len(items))
	for i, c := range items {
		ids[i] = c.ID
	}
	return ids
}
