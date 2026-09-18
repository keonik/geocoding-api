package services

import (
	"testing"
)

// The whole point. Every limit used to belong to the user, so a staging key
// running a load test spent production's allowance and the production
// integration started returning 429s. A cap on the staging key contains it.
func TestCappedKeyCannotSpendPastItsOwnLimit(t *testing.T) {
	db := setupKeyQuotaDB(t)

	cap := 5
	if err := Auth.SetKeyLimits(1, 2, nil, &cap); err != nil {
		t.Fatalf("cap staging key: %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := Auth.RecordUsage(1, 2, "geocode", "GET", 200, 5, "203.0.113.7", "probe", true, 1); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	staging, err := Auth.CheckKeyLimit(2)
	if err != nil {
		t.Fatalf("check staging: %v", err)
	}
	if staging.Exceeded != ScopeKeyDaily {
		t.Errorf("staging key after 5 of 5 = %q, want %q", staging.Exceeded, ScopeKeyDaily)
	}

	// Production has no cap of its own and is untouched by staging's spend.
	prod, err := Auth.CheckKeyLimit(1)
	if err != nil {
		t.Fatalf("check prod: %v", err)
	}
	if prod.HasCap() {
		t.Error("production key reports a cap it was never given")
	}
	if prod.Exceeded != "" {
		t.Errorf("production key reports %q after only staging was used", prod.Exceeded)
	}

	_ = db
}

// A key with no cap of its own must behave exactly as before this feature:
// never rejected at the key level, drawing on its owner's plan.
func TestUncappedKeyIsNeverRejectedAtTheKeyLevel(t *testing.T) {
	setupKeyQuotaDB(t)

	for i := 0; i < 50; i++ {
		if err := Auth.RecordUsage(1, 1, "geocode", "GET", 200, 5, "203.0.113.7", "probe", true, 1); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	status, err := Auth.CheckKeyLimit(1)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if status.HasCap() || status.Exceeded != "" {
		t.Errorf("an uncapped key reports cap=%t exceeded=%q", status.HasCap(), status.Exceeded)
	}
}

// Setting a cap is scoped to the owner in the write itself, so there is no gap
// between an ownership check and the update. Missing and not-yours answer the
// same, so the endpoint cannot be used to discover which key ids exist.
func TestOnlyTheOwnerCanCapAKey(t *testing.T) {
	setupKeyQuotaDB(t)

	cap := 10
	if err := Auth.SetKeyLimits(2, 1, &cap, nil); err == nil {
		t.Error("user 2 capped user 1's key")
	}
	if err := Auth.SetKeyLimits(1, 999, &cap, nil); err == nil {
		t.Error("a cap was set on a key that does not exist")
	}

	errOther := Auth.SetKeyLimits(2, 1, &cap, nil)
	errMissing := Auth.SetKeyLimits(1, 999, &cap, nil)
	if errOther == nil || errMissing == nil || errOther.Error() != errMissing.Error() {
		t.Errorf("not-yours (%v) and missing (%v) answer differently, which reveals which ids exist",
			errOther, errMissing)
	}
}

// Zero or negative would lock a key out entirely while reading like a setting.
func TestNonPositiveCapsAreRejected(t *testing.T) {
	setupKeyQuotaDB(t)

	for _, v := range []int{0, -5} {
		bad := v
		if err := Auth.SetKeyLimits(1, 1, &bad, nil); err == nil {
			t.Errorf("a monthly cap of %d was accepted", v)
		}
	}
}

// Clearing a cap returns the key to drawing on its owner's plan.
func TestClearingACapRestoresThePlan(t *testing.T) {
	setupKeyQuotaDB(t)

	cap := 1
	if err := Auth.SetKeyLimits(1, 1, &cap, &cap); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := Auth.RecordUsage(1, 1, "geocode", "GET", 200, 5, "203.0.113.7", "probe", true, 1); err != nil {
		t.Fatalf("record: %v", err)
	}
	if s, _ := Auth.CheckKeyLimit(1); s.Exceeded == "" {
		t.Fatal("setup: the key should be over its cap of 1")
	}

	if err := Auth.SetKeyLimits(1, 1, nil, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	s, err := Auth.CheckKeyLimit(1)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if s.HasCap() || s.Exceeded != "" {
		t.Errorf("after clearing, cap=%t exceeded=%q; the key should be uncapped", s.HasCap(), s.Exceeded)
	}
}

// A batch spends many units at once, so its size is checked against what the
// key has left, not just whether it has any.
func TestRemainingIsTheTighterOfTheTwoCaps(t *testing.T) {
	monthly, daily := 100, 10
	s := &KeyLimitStatus{MonthlyLimit: &monthly, DailyLimit: &daily, MonthlyUsage: 40, DailyUsage: 7}
	if r, ok := s.Remaining(); !ok || r != 3 {
		t.Errorf("remaining = (%d, %t), want (3, true): the daily cap is the tighter one", r, ok)
	}

	none := &KeyLimitStatus{}
	if _, ok := none.Remaining(); ok {
		t.Error("an uncapped key reported a remaining allowance")
	}
}

// Before migration 25 lands there are no cap columns and no counter table.
// Migrations run asynchronously while the server serves, and that window has
// taken this API down three times already -- so the check must degrade to "no
// cap", which is exactly the behaviour before this feature existed.
func TestKeyLimitDegradesBeforeTheMigration(t *testing.T) {
	db := setupKeyQuotaDB(t)

	for _, stmt := range []string{
		"DROP TABLE api_key_counters",
		"ALTER TABLE api_keys DROP COLUMN monthly_limit",
		"ALTER TABLE api_keys DROP COLUMN daily_limit",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("rewind %q: %v", stmt, err)
		}
	}

	status, err := Auth.CheckKeyLimit(1)
	if err != nil {
		t.Fatalf("key limit check failed before the migration: %v -- this 500s every request", err)
	}
	if status.HasCap() || status.Exceeded != "" {
		t.Error("before the migration a key should read as uncapped")
	}

	// Recording must not fail either: it runs on every request.
	if err := Auth.RecordUsage(1, 1, "geocode", "GET", 200, 5, "203.0.113.7", "probe", true, 1); err != nil {
		t.Errorf("recording usage failed before the migration: %v", err)
	}
}
