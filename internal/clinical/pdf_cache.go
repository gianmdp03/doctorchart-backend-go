package clinical

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const (
	pdfCacheMaxEntries = 64
	pdfCacheMaxBytes   = 16 << 20
	pdfCacheTTL        = 5 * time.Minute

	pdfRateWindow = time.Minute
	pdfRateLimit  = 40
)

type pdfCacheEntry struct {
	key       string
	data      []byte
	expiresAt time.Time
}

type pdfCache struct {
	mu         sync.Mutex
	maxEntries int
	maxBytes   int
	ttl        time.Duration
	totalBytes int
	order      *list.List
	items      map[string]*list.Element
}

func newPDFCache(maxEntries, maxBytes int, ttl time.Duration) *pdfCache {
	return &pdfCache{
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		ttl:        ttl,
		order:      list.New(),
		items:      make(map[string]*list.Element),
	}
}

func (c *pdfCache) get(key string, now time.Time) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry := elem.Value.(*pdfCacheEntry)
	if !now.Before(entry.expiresAt) {
		c.removeElement(elem)
		return nil, false
	}
	c.order.MoveToFront(elem)
	out := make([]byte, len(entry.data))
	copy(out, entry.data)
	return out, true
}

func (c *pdfCache) put(key string, data []byte, now time.Time) {
	if len(data) == 0 || len(data) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.removeElement(elem)
	}
	for (len(c.items) >= c.maxEntries || c.totalBytes+len(data) > c.maxBytes) && c.order.Len() > 0 {
		c.removeElement(c.order.Back())
	}
	stored := make([]byte, len(data))
	copy(stored, data)
	entry := &pdfCacheEntry{
		key:       key,
		data:      stored,
		expiresAt: now.Add(c.ttl),
	}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
	c.totalBytes += len(stored)
}

func (c *pdfCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*pdfCacheEntry)
	delete(c.items, entry.key)
	c.order.Remove(elem)
	c.totalBytes -= len(entry.data)
}

type pdfLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	buckets map[string][]time.Time
}

func newPDFLimiter(window time.Duration, limit int) *pdfLimiter {
	return &pdfLimiter{
		window:  window,
		limit:   limit,
		buckets: make(map[string][]time.Time),
	}
}

func (l *pdfLimiter) allow(userID string, now time.Time) bool {
	if userID == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	hits := l.buckets[userID]
	kept := hits[:0]
	for _, ts := range hits {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= l.limit {
		l.buckets[userID] = kept
		return false
	}
	kept = append(kept, now)
	l.buckets[userID] = kept
	return true
}

func pdfCacheKey(doc printDocument, kind string, zone *time.Location) string {
	h := sha256.New()
	specialty := ""
	if doc.Author.Specialty != nil {
		specialty = *doc.Author.Specialty
	}
	sigSHA, sigStamp := "", ""
	if doc.signature != nil {
		sigSHA = doc.signature.SHA256
		sigStamp = doc.signature.StampText
	}
	logoSHA := ""
	if doc.logo != nil {
		logoSHA = doc.logo.SHA256
	}
	localDate := doc.at.In(zone).Format("2006-01-02")
	fmt.Fprintf(h, "%s|%s|%d|%d|%s|%t|%s|%s|%s|%s|%s|%s|%s",
		doc.Author.UserID,
		doc.Source,
		doc.ID,
		doc.version,
		kind,
		doc.emitted,
		localDate,
		doc.Author.name,
		doc.Author.license,
		specialty,
		sigSHA,
		sigStamp,
		logoSHA,
	)
	return hex.EncodeToString(h.Sum(nil))
}
