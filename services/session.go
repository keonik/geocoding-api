package services

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// A session is one address lookup as the user experiences it: they type, the
// typeahead queries after each keystroke, and they pick a result. Billing
// each keystroke charges twenty calls for one lookup, which makes a typeahead
// the most expensive thing a customer can build on this API and pushes them
// towards debouncing it into uselessness or fetching a whole city's addresses
// up front.
//
// A session token collapses that into one billed lookup. The caller generates
// a token, sends it with every keystroke, and stops using it once the user
// picks a result.
const (
	// SessionTTL is how long a token stays one session. Long enough to type
	// an address into a slow form, short enough that a token left in a
	// client's state does not go on collecting free lookups all afternoon.
	SessionTTL = 2 * time.Minute

	// SessionMaxCalls bounds one session. A long address behind a debounce is
	// a handful of calls; twenty is generous. Past it the session is over and
	// the next call starts a new, billed one, so the most a token can be worth
	// is twenty lookups for one unit.
	SessionMaxCalls = 20

	// sessionSweepEvery bounds how often expired sessions are cleared out.
	sessionSweepEvery = time.Minute

	// maxSessions caps what a flood of invented tokens can cost in memory.
	// Past it new tokens are billed per call and not remembered: the caller
	// is charged exactly as they were before sessions existed, which is the
	// safe direction to fail.
	maxSessions = 50000
)

// sessionTokenPattern is deliberately narrow. The token is a client-chosen
// opaque string that ends up in a map key and in logs; a UUID passes, a essay
// does not.
var sessionTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{8,64}$`)

// ValidSessionToken reports whether a token is well formed.
func ValidSessionToken(token string) bool {
	return sessionTokenPattern.MatchString(token)
}

// SessionEligible reports whether a path bills by session.
//
// Only the autocomplete endpoint. A session stands for one lookup that a
// person is typing towards; letting any endpoint take one would sell twenty
// unrelated lookups for the price of one.
func SessionEligible(path string) bool {
	return strings.Contains(path, "/addresses/search")
}

// SessionState is what a caller is told about their session.
type SessionState struct {
	// Billed is whether this call was charged. True for the first call of a
	// session and for any call that could not join one.
	Billed bool
	// Remaining is how many more calls this session can carry.
	Remaining int
	// ExpiresIn is how long the session has left.
	ExpiresIn time.Duration
}

// SessionTracker remembers open sessions.
//
// In memory, like the burst limiter, and for the same reason: this decides
// whether to bill a keystroke, and asking a database on every keystroke would
// cost more than the keystroke does. The consequence is that with more than
// one instance a session can be billed once per instance the caller's
// keystrokes land on.
type SessionTracker struct {
	mu        sync.Mutex
	sessions  map[sessionKey]*sessionEntry
	lastSweep time.Time
}

type sessionKey struct {
	apiKeyID int
	token    string
}

type sessionEntry struct {
	startedAt time.Time
	calls     int
}

// NewSessionTracker returns an empty tracker.
func NewSessionTracker() *SessionTracker {
	return &SessionTracker{sessions: map[sessionKey]*sessionEntry{}}
}

// Sessions is the process-wide tracker.
var Sessions = NewSessionTracker()

// Count records a call against a session and reports whether to bill it.
//
// A token is scoped to the API key that used it: one customer's token cannot
// join another's session, and a leaked token buys nothing without the key.
func (t *SessionTracker) Count(apiKeyID int, token string, now time.Time) SessionState {
	k := sessionKey{apiKeyID: apiKeyID, token: token}

	t.mu.Lock()
	defer t.mu.Unlock()

	if now.Sub(t.lastSweep) >= sessionSweepEvery {
		for key, e := range t.sessions {
			if now.Sub(e.startedAt) > SessionTTL {
				delete(t.sessions, key)
			}
		}
		t.lastSweep = now
	}

	e, ok := t.sessions[k]
	switch {
	case ok && now.Sub(e.startedAt) <= SessionTTL && e.calls < SessionMaxCalls:
		// Inside an open session: the lookup was already paid for.
		e.calls++
		return SessionState{
			Billed:    false,
			Remaining: SessionMaxCalls - e.calls,
			ExpiresIn: SessionTTL - now.Sub(e.startedAt),
		}
	case ok:
		// Expired or spent. The next call is a new lookup and is billed.
		e.startedAt, e.calls = now, 1
	default:
		if len(t.sessions) >= maxSessions {
			// Nothing is remembered, so every call bills -- the behaviour
			// from before sessions existed.
			return SessionState{Billed: true, Remaining: 0}
		}
		t.sessions[k] = &sessionEntry{startedAt: now, calls: 1}
	}

	return SessionState{
		Billed:    true,
		Remaining: SessionMaxCalls - 1,
		ExpiresIn: SessionTTL,
	}
}

// Size reports how many sessions are held, for the tests.
func (t *SessionTracker) Size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}
