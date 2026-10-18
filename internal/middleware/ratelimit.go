package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
	r        rate.Limit
	b        int
}

func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*ipLimiter),
		r:        r,
		b:        b,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	for range time.Tick(5 * time.Minute) {
		rl.mu.Lock()
		for ip, l := range rl.limiters {
			if time.Since(l.lastSeen) > 10*time.Minute {
				delete(rl.limiters, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) get(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if l, ok := rl.limiters[ip]; ok {
		l.lastSeen = time.Now()
		return l.limiter
	}
	l := rate.NewLimiter(rl.r, rl.b)
	rl.limiters[ip] = &ipLimiter{limiter: l, lastSeen: time.Now()}
	return l
}

// clientKey returns the rate-limit key for a request. Only r.RemoteAddr is
// used because X-Forwarded-For can be forged. IPv6 clients are grouped by /64
// so a single host cannot dodge the limit by rotating addresses.
func clientKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

var (
	trustedProxiesMu sync.RWMutex
	trustedProxies   []*net.IPNet
)

// SetTrustedProxies configures which peers may report the real client address
// through X-Forwarded-For (comma-separated IPs or CIDRs, e.g. "172.18.0.0/16").
// Without it, only the TCP peer address is used because the header is forgeable.
func SetTrustedProxies(list string) error {
	var nets []*net.IPNet
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			if ip := net.ParseIP(item); ip != nil && ip.To4() != nil {
				item += "/32"
			} else {
				item += "/128"
			}
		}
		_, n, err := net.ParseCIDR(item)
		if err != nil {
			return fmt.Errorf("invalid trusted proxy %q: %w", item, err)
		}
		nets = append(nets, n)
	}
	trustedProxiesMu.Lock()
	trustedProxies = nets
	trustedProxiesMu.Unlock()
	return nil
}

func isTrustedProxy(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	trustedProxiesMu.RLock()
	defer trustedProxiesMu.RUnlock()
	for _, n := range trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// requestClient returns the client address for rate limiting. X-Forwarded-For
// is only honoured when the direct peer is a trusted proxy, and it is read
// from the right so entries added by the client itself are ignored.
func requestClient(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if isTrustedProxy(host) {
		parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(parts[i])
			if candidate == "" || net.ParseIP(candidate) == nil {
				continue
			}
			if !isTrustedProxy(candidate) {
				return clientKey(candidate)
			}
		}
	}
	return clientKey(host)
}

// AllowRequest checks the rate limit for r's client address.
func (rl *RateLimiter) AllowRequest(r *http.Request) bool {
	return rl.get(requestClient(r)).Allow()
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.AllowRequest(r) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
