package auth

import (
	"sync"
	"time"

	"pulse/internal/clock"
)

// Limiter es un "token bucket" en memoria por clave (IP, usuario, sesión).
type Limiter struct {
	mu      sync.Mutex
	clock   clock.Clock
	rate    float64 // fichas por segundo
	burst   float64
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter permite `burst` intentos seguidos y recupera `per` intentos por `every`.
func NewLimiter(c clock.Clock, burst int, per int, every time.Duration) *Limiter {
	return &Limiter{clock: c, burst: float64(burst), rate: float64(per) / every.Seconds(), buckets: map[string]*bucket{}}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last).Seconds()*l.rate > l.burst {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// LockoutUntil calcula el bloqueo progresivo: desde el 5.º fallo, 1, 2, 4… minutos, máximo 1 h.
func LockoutUntil(now time.Time, failures int) (time.Time, bool) {
	if failures < 5 {
		return time.Time{}, false
	}
	d := time.Minute << (failures - 5)
	if d > time.Hour || d <= 0 {
		d = time.Hour
	}
	return now.Add(d), true
}
