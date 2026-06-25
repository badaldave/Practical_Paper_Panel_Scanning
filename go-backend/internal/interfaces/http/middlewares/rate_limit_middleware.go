package middlewares

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type client struct {
	lastSeen time.Time
	tokens   float64
}

// Let's implement standard token bucket
type RateLimiter struct {
	sync.Mutex
	ips    map[string]*client
	rate   float64 // tokens per second
	bucket float64 // capacity
}

func NewRateLimiter(rate float64, capacity float64) *RateLimiter {
	rl := &RateLimiter{
		ips:    make(map[string]*client),
		rate:   rate,
		bucket: capacity,
	}

	// Clean up old clients periodically
	go func() {
		for {
			time.Sleep(1 * time.Minute)
			rl.Lock()
			for ip, cl := range rl.ips {
				if time.Since(cl.lastSeen) > 3*time.Minute {
					delete(rl.ips, ip)
				}
			}
			rl.Unlock()
		}
	}()

	return rl
}

func (rl *RateLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		rl.Lock()
		cl, exists := rl.ips[ip]
		now := time.Now()

		if !exists {
			rl.ips[ip] = &client{
				lastSeen: now,
				tokens:   rl.bucket,
			}
			rl.Unlock()
			c.Next()
			return
		}

		// Calculate tokens to add
		elapsed := now.Sub(cl.lastSeen).Seconds()
		cl.lastSeen = now
		cl.tokens += elapsed * rl.rate
		if cl.tokens > rl.bucket {
			cl.tokens = rl.bucket
		}

		if cl.tokens < 1.0 {
			rl.Unlock()
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Rate limit exceeded. Please try again later."})
			c.Abort()
			return
		}

		cl.tokens -= 1.0
		rl.Unlock()
		c.Next()
	}
}
