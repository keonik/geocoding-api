package services

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// The point of the thing: one lookup billed, however many keystrokes it took.
func TestSessionBillsOnceForTheKeystrokes(t *testing.T) {
	tr := NewSessionTracker()
	now := time.Now()
	const key, token = 1, "8f14e45f-ea8d-4c1b-9b47-1f4b0ac96b23"

	first := tr.Count(key, token, now)
	if !first.Billed {
		t.Fatal("the first call of a session was not billed")
	}
	if first.Remaining != SessionMaxCalls-1 {
		t.Errorf("remaining = %d, want %d", first.Remaining, SessionMaxCalls-1)
	}

	// Typing.
	for i := 2; i <= SessionMaxCalls; i++ {
		state := tr.Count(key, token, now.Add(time.Duration(i)*100*time.Millisecond))
		if state.Billed {
			t.Fatalf("keystroke %d was billed inside an open session", i)
		}
		if state.Remaining != SessionMaxCalls-i {
			t.Errorf("call %d: remaining = %d, want %d", i, state.Remaining, SessionMaxCalls-i)
		}
	}

	// Past the cap the session is over, so the next call is a new lookup.
	if state := tr.Count(key, token, now.Add(3*time.Second)); !state.Billed {
		t.Error("a call past the cap was not billed")
	}
}

// A token left in a client's state must not go on collecting free lookups.
func TestSessionExpires(t *testing.T) {
	tr := NewSessionTracker()
	now := time.Now()
	const key, token = 1, "session-token-expiry"

	tr.Count(key, token, now)
	if state := tr.Count(key, token, now.Add(SessionTTL-time.Second)); state.Billed {
		t.Error("a call inside the window was billed")
	}
	if state := tr.Count(key, token, now.Add(SessionTTL+time.Second)); !state.Billed {
		t.Error("a call after the window was not billed as a new session")
	}
}

// A token is scoped to the key that used it: one customer cannot ride
// another's session, and a leaked token is worth nothing without the key.
func TestSessionIsScopedToTheKey(t *testing.T) {
	tr := NewSessionTracker()
	now := time.Now()
	const token = "shared-token-value"

	tr.Count(1, token, now)
	if state := tr.Count(2, token, now); !state.Billed {
		t.Error("another key's call joined the session for free")
	}
}

// Invented tokens must not cost unbounded memory. Past the cap they are
// billed per call and not remembered, which is how it behaved before
// sessions existed.
func TestSessionTrackerIsBounded(t *testing.T) {
	tr := NewSessionTracker()
	now := time.Now()
	for i := 0; i < maxSessions+100; i++ {
		tr.Count(1, fmt.Sprintf("token-%06d-aaaa", i), now)
	}
	if tr.Size() > maxSessions {
		t.Errorf("held %d sessions, want no more than %d", tr.Size(), maxSessions)
	}
	// Beyond the cap a caller is charged exactly as before.
	if state := tr.Count(1, "token-overflow-xyz", now); !state.Billed {
		t.Error("a call past the memory cap was not billed")
	}
}

// Expired sessions are cleared out rather than held for the life of the
// process.
func TestSessionSweepForgetsExpired(t *testing.T) {
	tr := NewSessionTracker()
	now := time.Now()
	for i := 0; i < 100; i++ {
		tr.Count(1, fmt.Sprintf("token-sweep-%03d", i), now)
	}
	if tr.Size() != 100 {
		t.Fatalf("held %d sessions, want 100", tr.Size())
	}
	tr.Count(1, "token-sweep-later", now.Add(SessionTTL+sessionSweepEvery+time.Second))
	if tr.Size() != 1 {
		t.Errorf("held %d sessions after the sweep, want 1", tr.Size())
	}
}

func TestSessionTokenValidation(t *testing.T) {
	valid := []string{
		"8f14e45f-ea8d-4c1b-9b47-1f4b0ac96b23",
		"abcd1234",
		"session.token:1",
	}
	for _, token := range valid {
		if !ValidSessionToken(token) {
			t.Errorf("%q was rejected", token)
		}
	}
	invalid := []string{
		"",
		"short",                          // under 8
		string(make([]byte, 65)),         // over 64, and not printable
		"has spaces in it",               // would land in logs oddly
		"tok/../../etc",                  // path-shaped
		"<script>alert(1)</script>xxxxx", // markup
	}
	for _, token := range invalid {
		if ValidSessionToken(token) {
			t.Errorf("%q was accepted", token)
		}
	}
}

// Only the autocomplete endpoint bills by session: a session stands for one
// lookup being typed towards, and any other endpoint taking one would sell
// twenty unrelated lookups for the price of one.
func TestSessionEligibility(t *testing.T) {
	if !SessionEligible("/api/v1/addresses/search") {
		t.Error("the autocomplete endpoint should take a session")
	}
	for _, path := range []string{
		"/api/v1/addresses",
		"/api/v1/geocode/43215",
		"/api/v1/geocode/batch",
		"/api/v1/reverse",
		"/api/v1/enrich",
	} {
		if SessionEligible(path) {
			t.Errorf("%s should not bill by session", path)
		}
	}
}

// One session, many concurrent keystrokes: exactly one call is billed.
func TestSessionUnderConcurrency(t *testing.T) {
	tr := NewSessionTracker()
	now := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	billed := 0
	for i := 0; i < SessionMaxCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tr.Count(9, "concurrent-session-token", now).Billed {
				mu.Lock()
				billed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if billed != 1 {
		t.Errorf("%d calls billed, want exactly 1", billed)
	}
}
