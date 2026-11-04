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
//
// Expired entries go at the next use of the cache, whichever feed it's for:
// most feeds are bots' one-off requests, and nothing else would remove those
// until the size limit did, months later.

import (
	"container/list"
	"sync"
	"time"
)

const (
	feedCacheTTL      = 60 * time.Second
	feedCacheMaxBytes = 32 << 20
	// feedEntryOverhead approximates an entry's memory besides its body
	// and key strings (the list elements, map entry and struct).
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
	queued       *list.Element // its element in feedCache.queue
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
	// queue holds the *feedEntry values, oldest put first. That's also expiry
	// order, since the TTL is constant and put() re-inserts, while lru isn't,
	// since get() reorders it. A queue rather than a full sweep every TTL: a
	// sweep would leave a dead entry for up to another TTL (and in stats()),
	// and this costs a poll one comparison unless something has expired.
	queue *list.List
	bytes int
	// gen counts invalidations. A feed read before a save finished mustn't
	// be stored after the save dropped the old entry, so put() takes the
	// gen from before the read and stores nothing if it changed.
	gen uint64
}

func newFeedCache(maxBytes int, ttl time.Duration, now func() time.Time) *feedCache {
	return &feedCache{maxBytes: maxBytes, ttl: ttl, now: now,
		entries: map[feedKey]*list.Element{}, lru: list.New(), queue: list.New()}
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
	c.expire()
	el, ok := c.entries[key]
	if !ok {
		return nil
	}
	c.lru.MoveToFront(el)
	return el.Value.(*feedEntry)
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
	// Before evicting below, so that dead entries go instead of live ones.
	c.expire()
	if el, ok := c.entries[key]; ok {
		c.remove(el)
	}
	e.expires = c.now().Add(c.ttl)
	e.queued = c.queue.PushBack(e)
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

// stats returns the cache's size: its entries and their bytes (as size()
// counts them).
func (c *feedCache) stats() (entries, bytes int) {
	if c == nil {
		return 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire()
	return len(c.entries), c.bytes
}

// expire removes the entries whose time is up, so nothing else needs to check
// expires. It can stop at the first live entry because now() doesn't go back
// (time.Now is monotonic).
func (c *feedCache) expire() {
	now := c.now()
	for el := c.queue.Front(); el != nil; el = c.queue.Front() {
		e := el.Value.(*feedEntry)
		if now.Before(e.expires) {
			return
		}
		c.remove(c.entries[e.key])
	}
}

func (c *feedCache) remove(el *list.Element) {
	e := c.lru.Remove(el).(*feedEntry)
	c.queue.Remove(e.queued)
	delete(c.entries, e.key)
	c.bytes -= e.size()
}
