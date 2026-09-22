package services

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// burstKeyTTL is how long an idle key's bucket is kept. Longer than the time
// a full bucket takes to refill, so a bucket is never discarded while it
// still holds a deficit -- dropping it early would hand a throttled caller a
// fresh allowance by waiting.
const burstKeyTTL = 5 * time.Minute

// burstSweepEvery bounds how often the map is swept. The sweep walks every
// entry, so doing it per request would make the limiter's cost grow with the
// number of keys it is protecting against.
const burstSweepEvery = time.Minute

// BurstLimiters holds one token bucket per API key.
//
// Per key, not per user and not per IP: a key is the thing a caller
// integrates with, and one runaway script should not throttle its owner's
// other keys. The state is in memory, so each instance enforces its own
// share -- with one instance that is the whole limit, and with several the
// effective ceiling is that many times the rate. That is the right trade for
// a guard whose job is to shed load cheaply: a shared counter would put a
// network round trip in front of every request to protect against floods.
type BurstLimiters struct {
	mu        sync.Mutex
	entries   map[int]*burstEntry
	lastSweep time.Time
}

type burstEntry struct {
	limiter *rate.Limiter
	// perSecond is what the bucket was built for. A plan change alters the
	// allowance, and comparing it here rebuilds the bucket rather than
	// letting a key keep the rate it first arrived with.
	perSecond int
	lastSeen  time.Time
}

func NewBurstLimiters() *BurstLimiters {
	return &BurstLimiters{entries: map[int]*burstEntry{}}
}

// allow reports whether this key may make a call now, and how long until it
// could. The wait is zero when the call is allowed.
//
// n is how many lookups the call is worth: one for an ordinary request, the
// item count for a batch. perSecond <= 0 means no burst limit, which is how a
// deployment opts out entirely.
func (b *BurstLimiters) AllowN(keyID, perSecond, n int, now time.Time) (bool, time.Duration) {
	if perSecond <= 0 {
		return true, 0
	}
	if n < 1 {
		n = 1
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if now.Sub(b.lastSweep) >= burstSweepEvery {
		for id, e := range b.entries {
			if now.Sub(e.lastSeen) > burstKeyTTL {
				delete(b.entries, id)
			}
		}
		b.lastSweep = now
	}

	e, ok := b.entries[keyID]
	if !ok || e.perSecond != perSecond {
		e = &burstEntry{
			limiter:   rate.NewLimiter(rate.Limit(perSecond), BurstDepthFor(perSecond)),
			perSecond: perSecond,
		}
		b.entries[keyID] = e
	}
	e.lastSeen = now

	reservation := e.limiter.ReserveN(now, n)
	if !reservation.OK() {
		// The bucket is smaller than the request and no wait would ever
		// satisfy it. BurstDepthFor keeps a full batch inside the depth, so
		// this needs a caller asking for more than MaxBatchItems at once.
		return false, time.Second
	}
	if wait := reservation.DelayFrom(now); wait > 0 {
		// Not taking the token: a rejected request must not spend the
		// allowance of the retry that follows it. CancelAt, not Cancel:
		// Cancel returns the token as of time.Now(), which is a different
		// instant from the one the reservation was made at and leaves the
		// bucket holding a fraction more or less than it should.
		reservation.CancelAt(now)
		return false, wait
	}
	return true, 0
}

// size reports how many buckets are held, for the tests.
func (b *BurstLimiters) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

// KeyBursts is the process-wide limiter. The middleware charges one token
// per request; a handler that does more work than that charges the rest.
var KeyBursts = NewBurstLimiters()

// BurstDepthFor is how many tokens a key's bucket holds.
//
// The bucket counts lookups rather than requests, so a batch costs what it
// asks for. That makes the depth a question about the largest legitimate
// arrival: one full batch. Below that, a caller on a small plan could never
// submit one at all, however long they waited -- the bucket would refuse a
// request it could never hold.
func BurstDepthFor(perSecond int) int {
	if perSecond < MaxBatchItems {
		return MaxBatchItems
	}
	return perSecond
}
