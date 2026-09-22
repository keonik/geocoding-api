package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// A batch is charged for its items, not for being one request.
//
// The refusal happens before any lookup, so this needs no database -- which
// is the point: the work is what is being refused.
func TestBatchIsChargedByItsItems(t *testing.T) {
	const keyID, perSecond = 990001, 25

	// Drain this key's allowance, as a first full batch would.
	now := time.Now()
	if ok, _ := services.KeyBursts.AllowN(keyID, perSecond, services.BurstDepthFor(perSecond), now); !ok {
		t.Fatal("could not drain a fresh bucket")
	}

	items := make([]string, services.MaxBatchItems)
	for i := range items {
		items[i] = `{"zip_code":"43215"}`
	}
	body := `{"items":[` + strings.Join(items, ",") + `]}`

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/geocode/batch", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(services.BurstLimitKey, perSecond)
	c.Set("api_key", &models.APIKey{ID: keyID})

	if err := BatchGeocodeHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: a second full batch cost one token instead of its items", rec.Code)
	}
	if got := rec.Header().Get("X-RateLimit-Scope"); got != services.ScopeBurst {
		t.Errorf("X-RateLimit-Scope = %q", got)
	}
	// 100 lookups at 25/s is four seconds, so the wait is real rather than
	// the one-second floor.
	if got := rec.Header().Get("Retry-After"); got != "4" {
		t.Errorf("Retry-After = %q, want \"4\"", got)
	}

	var parsed struct {
		Data struct {
			Items      int    `json:"items"`
			Limit      int    `json:"limit"`
			LimitScope string `json:"limit_scope"`
			RetryAfter int    `json:"retry_after"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Data.Items != services.MaxBatchItems || parsed.Data.Limit != perSecond ||
		parsed.Data.LimitScope != services.ScopeBurst || parsed.Data.RetryAfter != 4 {
		t.Errorf("body = %+v", parsed.Data)
	}
}

// A key with no burst limit published on the context -- an unauthenticated
// path, or the guard switched off -- is not charged and not refused here.
func TestBatchWithoutABurstLimitIsNotCharged(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/geocode/batch",
		strings.NewReader(`{"items":[{"zip_code":"43215"},{"zip_code":"43215"}]}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// No BurstLimitKey, no api_key: nothing to charge against.
	c.Set(services.BurstLimitKey, 0)

	// It gets past the burst check and fails later for want of a database,
	// which is what "not refused here" looks like from outside.
	func() {
		defer func() { recover() }()
		BatchGeocodeHandler(c)
	}()
	if rec.Code == http.StatusTooManyRequests {
		t.Error("a batch was burst-refused with no limit in force")
	}
}
