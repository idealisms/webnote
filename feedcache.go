package main

// An in-memory cache of rendered RSS feeds. Feed readers poll often (one
// every 8 seconds), and each uncached feed costs 2 Datastore reads (the
// workspace and its latest notes). The Python version had ndb's memcache in
// front of these reads; with max_instances: 1, memory in the one instance
// does the same job.
//
// Entries last feedCacheTTL, the max-age feeds are sent with, and a save
// drops its workspace's entries, so the instance that took the save serves
// the new feed at once. Another instance (during a deploy or restart) can
// serve a feed up to feedCacheTTL old.

import (
	"container/list"
	"sync"
	"time"
)

const (
	feedCacheTTL      = 60 * time.Second
	feedCacheMaxBytes = 32 << 20
	// feedEntryOverhead approximates an entry's memory besides its body
	// and key strings (the list element, map entry and struct).
	feedEntryOverhead = 200
)

// feedKey is a feed's host URL (the feed's links include it) and its
// workspace's key name (workspaceKeyID()), so every spelling of a name that
// opens the same workspace shares an entry.
type feedKey struct{ host, name string }

type feedEntry struct {
	key          feedKey
	found        bool   // false: no such workspace (a 404)
	body         []byte // the rendered feed, if found
	lastModified time.Time
	expires      time.Time
}

func (e *feedEntry) size() int {
	return len(e.body) + len(e.key.host) + len(e.key.name) + feedEntryOverhead
}

// feedCache is safe for concurrent use. A nil *feedCache caches nothing.
type feedCache struct {
	mu       sync.Mutex
	maxBytes int
	ttl      time.Duration
	now      func() time.Time
	entries  map[feedKey]*list.Element // of *feedEntry
	lru      *list.List                // most recently used first
	bytes    int
	// gen counts invalidations. A feed read before a save finished mustn't
	// be stored after the save dropped the old entry, so put() takes the
	// gen from before the read and stores nothing if it changed.
	gen uint64
}

func newFeedCache(maxBytes int, ttl time.Duration, now func() time.Time) *feedCache {
	return &feedCache{maxBytes: maxBytes, ttl: ttl, now: now,
		entries: map[feedKey]*list.Element{}, lru: list.New()}
}

// generation is the value to pass to put() for a feed read from now on.
func (c *feedCache) generation() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// get returns the unexpired entry for key, or nil.
func (c *feedCache) get(key feedKey) *feedEntry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return nil
	}
	e := el.Value.(*feedEntry)
	if !c.now().Before(e.expires) {
		c.remove(el)
		return nil
	}
	c.lru.MoveToFront(el)
	return e
}

// put stores a feed read when generation() was gen, unless the cache was
// invalidated since or the entry alone is over the size limit. Older entries
// are evicted to make room.
func (c *feedCache) put(gen uint64, key feedKey, found bool, body []byte, lastModified time.Time) {
	if c == nil {
		return
	}
	e := &feedEntry{key: key, found: found, body: body, lastModified: lastModified}
	if e.size() > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	if el, ok := c.entries[key]; ok {
		c.remove(el)
	}
	e.expires = c.now().Add(c.ttl)
	c.entries[key] = c.lru.PushFront(e)
	c.bytes += e.size()
	for c.bytes > c.maxBytes {
		c.remove(c.lru.Back())
	}
}

// invalidate drops the feeds of the workspace whose key name is name, on
// every host.
func (c *feedCache) invalidate(name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	for key, el := range c.entries {
		if key.name == name {
			c.remove(el)
		}
	}
}

func (c *feedCache) remove(el *list.Element) {
	e := c.lru.Remove(el).(*feedEntry)
	delete(c.entries, e.key)
	c.bytes -= e.size()
}
