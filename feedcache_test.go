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
	before := c.bytes
	c.get(key("d"))
	if c.bytes >= before {
		t.Errorf("an expired entry wasn't removed: bytes %d", c.bytes)
	}

	// A nil cache caches nothing.
	var none *feedCache
	none.put(none.generation(), key("a"), true, body, now)
	none.invalidate("a")
	if none.get(key("a")) != nil {
		t.Errorf("nil cache returned an entry")
	}
}
