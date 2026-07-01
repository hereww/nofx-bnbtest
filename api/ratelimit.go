package api

import (
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rlBucket
	rate    float64
	burst   float64
	lastGC  time.Time
}

type rlBucket struct {
	tokens float64
	last   time.Time
}

func newIPRateLimiter(ratePerSec, burst float64) *ipRateLimiter {
	return &ipRateLimiter{
		buckets: make(map[string]*rlBucket),
		rate:    ratePerSec,
		burst:   burst,
	}
}

func (l *ipRateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.lastGC.IsZero() {
		l.lastGC = now
	}
	if now.Sub(l.lastGC) > time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &rlBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+elapsed*l.rate)
		b.last = now
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func rateLimitMiddleware(l *ipRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(c.ClientIP(), time.Now()) {
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please slow down and try again in a minute.",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
