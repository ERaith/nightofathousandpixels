package tmdb

import (
	"sync"
	"time"
)

// Why there is a cache here at all.
//
// Thirty people submit films over about a fortnight, and they submit the same
// films. The horror everyone has been told to watch gets searched a dozen
// times in one evening, and each of those is a person typing, so a single
// search for "hereditary" is eight or nine requests as the word appears one
// letter at a time. Without a cache, one group chat recommendation is a
// hundred calls to TMDB for one answer.
//
// What it is NOT is a database. It is in-process, so every replica has its own
// and nothing survives a restart, and that is the right size for the problem:
// the cost of a miss is one fast HTTP call, and the cost of getting caching
// wrong at any larger scale is a film whose title is stale for a day. Anything
// with a network hop in it would be more infrastructure than the thing it
// speeds up.
const (
	// searchTTL is how long a set of matches for a query is reused.
	//
	// Short, because a search is a live question: a film added to TMDB this
	// morning should be findable this afternoon, and somebody who has just
	// created the entry for the obscure thing they want to submit should not
	// have to wait a day for this site to see it. Ten minutes covers the
	// keystrokes of one person typing and the evening's worth of other people
	// searching the same recommendation, which is all it is for.
	searchTTL = 10 * time.Minute

	// detailTTL is how long one film's facts are reused.
	//
	// Long, because they are facts. A film released in 1982 will have the same
	// title, the same year and the same trailer tomorrow. The only things that
	// realistically change are a corrected synopsis and a newly added trailer,
	// and neither is worth a round trip on the submit path -- a person who
	// needs the new synopsis can edit the box, which is why it stays editable.
	detailTTL = 24 * time.Hour

	// maxCacheEntries bounds each cache.
	//
	// The bound is not about memory -- a thousand of these is well under a
	// megabyte -- it is about the fact that the search cache is keyed on text
	// somebody typed. Without a cap, a signed-in member could pin an unbounded
	// number of distinct queries in the process by holding a key down, and an
	// unbounded map behind an authenticated endpoint is a memory leak with a
	// user interface. A thousand is far more distinct searches than this group
	// will make in a season.
	maxCacheEntries = 1000
)

// cache is a TTL map guarded by a mutex.
//
// A mutex rather than sync.Map because both operations write: a get that finds
// an expired entry deletes it, so the read-mostly pattern sync.Map is built
// for does not hold here. Contention is not a consideration -- this is thirty
// people, not thirty thousand -- and a plain mutex is a thing anyone reading
// it can see the whole of.
type cache[T any] struct {
	ttl   time.Duration
	limit int

	mu      sync.Mutex
	entries map[string]entry[T]

	// now is time.Now, replaced in tests. It is a field rather than a package
	// variable so two tests can run in parallel with different clocks.
	now func() time.Time
}

type entry[T any] struct {
	value     T
	expiresAt time.Time
}

func newCache[T any](ttl time.Duration, limit int) *cache[T] {
	return &cache[T]{
		ttl:     ttl,
		limit:   limit,
		entries: make(map[string]entry[T]),
		now:     time.Now,
	}
}

// get returns a live value, or the zero value and false.
//
// An expired entry is deleted on the way past rather than left to a sweeper.
// That is what keeps this from needing a goroutine: the keys that are looked
// up are the ones that get cleaned, and the keys that are never looked up
// again are handled by the size cap instead.
func (c *cache[T]) get(key string) (T, bool) {
	var zero T

	if c == nil {
		return zero, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok {
		return zero, false
	}
	if !c.now().Before(e.expiresAt) {
		delete(c.entries, key)

		return zero, false
	}

	return e.value, true
}

// put stores a value for the cache's TTL.
func (c *cache[T]) put(key string, value T) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.limit {
		c.evict()
	}

	c.entries[key] = entry[T]{value: value, expiresAt: c.now().Add(c.ttl)}
}

// evict makes room. The caller holds the mutex.
//
// It drops everything expired first, and only if that freed nothing does it
// drop live entries. The second pass is a Go map range, so which entries go is
// unspecified -- deliberately, rather than for want of an LRU. An LRU here
// would mean a linked list and an access-order update on every read, to
// protect a cache whose miss costs one HTTP call, at a size the group will
// never reach; and the honest failure mode of picking at random is "somebody's
// search is a little slower once", which is the same failure mode as a cold
// start.
func (c *cache[T]) evict() {
	now := c.now()
	for key, e := range c.entries {
		if !now.Before(e.expiresAt) {
			delete(c.entries, key)
		}
	}

	if len(c.entries) < c.limit {
		return
	}

	// Still full: everything in it is live. Drop a tenth of it so this is not
	// re-entered on the very next put, which is what would happen if exactly
	// one entry were removed each time.
	drop := c.limit/10 + 1
	for key := range c.entries {
		if drop == 0 {
			return
		}

		delete(c.entries, key)
		drop--
	}
}

// len is the number of entries, expired ones included. Tests only.
func (c *cache[T]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.entries)
}
