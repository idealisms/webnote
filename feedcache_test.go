package main

import (
	"fmt"
	"testing"
	"time"
)

func TestFeedCache(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	c := newFeedCache(3*(100+feedEntryOverhead+len("h")+len("a")), time.Minute, func() time.Time { return now })
	key := func(name string) feedKey { return feedKey{"h", name} }
	body := make([]byte, 100)
	has := func(names ...string) string {
		var s string
		for _, n := range names {
			s += fmt.Sprint(c.get(key(n)) != nil, " ")
		}
		return s
	}

	for _, n := range []string{"a", "b", "c"} {
		c.put(c.generation(), key(n), true, body, now)
	}
	if got := has("a", "b", "c"); got != "true true true " {
		t.Fatalf("after 3 puts: %s", got)
	}
	// "a" was used last, so a fourth entry evicts "b".
	c.get(key("a"))
	c.put(c.generation(), key("d"), true, body, now)
	if got := has("a", "b", "c", "d"); got != "true false true true " {
		t.Errorf("after a fourth put: %s", got)
	}
	if c.bytes > c.maxBytes {
		t.Errorf("bytes %d over max %d", c.bytes, c.maxBytes)
	}

	// An entry bigger than the whole cache isn't stored and evicts nothing.
	c.put(c.generation(), key("big"), true, make([]byte, c.maxBytes), now)
	if got := has("big", "a", "c", "d"); got != "false true true true " {
		t.Errorf("after a too-big put: %s", got)
	}

	// Invalidating drops the name on every host and makes puts of feeds
	// read before it do nothing.
	gen := c.generation()
	c.put(gen, feedKey{"other", "a"}, false, nil, time.Time{})
	c.invalidate("a")
	if c.get(key("a")) != nil || c.get(feedKey{"other", "a"}) != nil {
		t.Errorf("a still cached after invalidate")
	}
	c.put(gen, key("a"), true, body, now)
	if c.get(key("a")) != nil {
		t.Errorf("a feed read before invalidate was stored")
	}

	// Entries expire after the TTL.
	now = now.Add(time.Minute - time.Nanosecond)
	if c.get(key("c")) == nil {
		t.Errorf("c expired early")
	}
	now = now.Add(time.Nanosecond)
	if c.get(key("c")) != nil {
		t.Errorf("c didn't expire")
	}
	// d, stored with c, went with it without being asked for.
	if entries, bytes := c.stats(); entries != 0 || bytes != 0 {
		t.Errorf("expired entries weren't removed: %d entries, %d bytes", entries, bytes)
	}

	// A nil cache caches nothing.
	var none *feedCache
	none.put(none.generation(), key("a"), true, body, now)
	none.invalidate("a")
	if none.get(key("a")) != nil {
		t.Errorf("nil cache returned an entry")
	}
	if entries, bytes := none.stats(); entries != 0 || bytes != 0 {
		t.Errorf("nil cache has %d entries, %d bytes", entries, bytes)
	}
}

// TestFeedCacheExpiry checks that entries go when their time is up, whether
// or not anything asks for them again.
func TestFeedCacheExpiry(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	newCache := func(maxBytes int) *feedCache {
		return newFeedCache(maxBytes, time.Minute, func() time.Time { return now })
	}
	key := func(name string) feedKey { return feedKey{"h", name} }
	put := func(c *feedCache, name string) { c.put(c.generation(), key(name), false, nil, time.Time{}) }
	entrySize := (&feedEntry{key: key("a")}).size()
	wantStats := func(t *testing.T, c *feedCache, entries int) {
		t.Helper()
		gotEntries, gotBytes := c.stats()
		if gotEntries != entries || gotBytes != entries*entrySize {
			t.Errorf("stats: %d entries, %d bytes; want %d, %d", gotEntries, gotBytes, entries, entries*entrySize)
		}
	}
	// has looks at the map, since get() would expire entries itself.
	has := func(c *feedCache, name string) bool {
		_, ok := c.entries[key(name)]
		return ok
	}

	t.Run("never read again", func(t *testing.T) {
		c := newCache(feedCacheMaxBytes)
		for _, n := range []string{"a", "b", "c"} {
			put(c, n)
		}
		wantStats(t, c, 3)
		now = now.Add(time.Minute)
		wantStats(t, c, 0)
		if c.lru.Len() != 0 || c.queue.Len() != 0 {
			t.Errorf("left in the lists: lru %d, queue %d", c.lru.Len(), c.queue.Len())
		}
	})

	t.Run("a put of another feed expires entries", func(t *testing.T) {
		c := newCache(feedCacheMaxBytes)
		put(c, "a")
		now = now.Add(time.Minute)
		put(c, "b")
		if has(c, "a") || !has(c, "b") {
			t.Errorf("a %v, b %v; want only b", has(c, "a"), has(c, "b"))
		}
	})

	t.Run("a get of another feed expires entries", func(t *testing.T) {
		c := newCache(feedCacheMaxBytes)
		put(c, "a")
		now = now.Add(time.Second)
		put(c, "b")
		now = now.Add(time.Minute - time.Second)
		if c.get(key("b")) == nil {
			t.Errorf("b expired early")
		}
		if has(c, "a") {
			t.Errorf("a wasn't removed")
		}
	})

	t.Run("unexpired entries survive", func(t *testing.T) {
		c := newCache(feedCacheMaxBytes)
		put(c, "a")
		now = now.Add(30 * time.Second)
		put(c, "b")
		now = now.Add(30*time.Second - time.Nanosecond)
		wantStats(t, c, 2)
		now = now.Add(time.Nanosecond)
		wantStats(t, c, 1)
		if has(c, "a") || c.get(key("b")) == nil {
			t.Errorf("a %v, b %v; want only b", has(c, "a"), has(c, "b"))
		}
		now = now.Add(30 * time.Second)
		wantStats(t, c, 0)
	})

	// Reading an entry puts it at the front of the LRU list, with an unexpired
	// entry behind it, but doesn't extend its life.
	t.Run("a recent get doesn't delay expiry", func(t *testing.T) {
		c := newCache(feedCacheMaxBytes)
		put(c, "a")
		now = now.Add(30 * time.Second)
		put(c, "b")
		now = now.Add(30*time.Second - time.Nanosecond)
		if c.get(key("a")) == nil {
			t.Fatalf("a expired early")
		}
		now = now.Add(time.Nanosecond)
		wantStats(t, c, 1)
		if has(c, "a") || !has(c, "b") {
			t.Errorf("a %v, b %v; want only b", has(c, "a"), has(c, "b"))
		}
	})

	t.Run("a put restarts the entry's time", func(t *testing.T) {
		c := newCache(feedCacheMaxBytes)
		put(c, "a")
		now = now.Add(30 * time.Second)
		put(c, "a")
		now = now.Add(30 * time.Second)
		wantStats(t, c, 1)
		now = now.Add(30 * time.Second)
		wantStats(t, c, 0)
	})

	// Entries that left early mustn't be left in the queue, to be removed
	// again when their time comes.
	t.Run("invalidated and evicted entries", func(t *testing.T) {
		c := newCache(2 * entrySize)
		put(c, "a")
		put(c, "b")
		c.invalidate("a")
		put(c, "c")
		put(c, "d") // evicts b
		if has(c, "b") {
			t.Errorf("b wasn't evicted")
		}
		wantStats(t, c, 2)
		if c.queue.Len() != 2 {
			t.Errorf("queue holds %d entries, want 2", c.queue.Len())
		}
		now = now.Add(time.Minute)
		wantStats(t, c, 0)
	})

	t.Run("dead entries go before live ones are evicted", func(t *testing.T) {
		c := newCache(2 * entrySize)
		put(c, "a")
		now = now.Add(30 * time.Second)
		put(c, "b")
		c.get(key("a")) // so b, though live, is the LRU list's last
		now = now.Add(30 * time.Second)
		put(c, "c")
		if has(c, "a") || !has(c, "b") || !has(c, "c") {
			t.Errorf("a %v, b %v, c %v; want b and c", has(c, "a"), has(c, "b"), has(c, "c"))
		}
	})
}
