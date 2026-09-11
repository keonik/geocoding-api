package services

import (
	"testing"

	"geocoding-api/models"
)

// A caller matching addresses automatically has to decide whether to accept a
// result. Before this, a trigram rescue that barely cleared the similarity
// threshold and an address that matched every word exactly came back
// indistinguishable, in one flat list.
func TestMatchTierDistinguishesExactFromTypo(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	exact, _, err := svc.SearchAddresses(models.AddressSearchParams{Query: "Barendt", Limit: 5})
	if err != nil {
		t.Fatalf("exact search: %v", err)
	}
	if len(exact) == 0 {
		t.Fatal("expected hits for a correctly spelled street")
	}

	// "barnedt" is a transposition; the prefix index cannot match it, so only
	// the trigram fallback can.
	typo, _, err := svc.SearchAddresses(models.AddressSearchParams{Query: "barendtt", Limit: 5})
	if err != nil {
		t.Fatalf("typo search: %v", err)
	}

	if exact[0].Match == nil {
		t.Fatal("match metadata missing on a search result")
	}
	if exact[0].Match.Tier != models.MatchTierPrefix {
		t.Errorf("exact hit tier = %q, want %q", exact[0].Match.Tier, models.MatchTierPrefix)
	}
	if exact[0].Match.Confidence == nil {
		t.Fatal("no confidence on a text search hit")
	}
	t.Logf("exact  %-34s tier=%s confidence=%.3f",
		exact[0].FullAddress, exact[0].Match.Tier, *exact[0].Match.Confidence)

	if len(typo) > 0 {
		if typo[0].Match == nil {
			t.Fatal("match metadata missing on the fuzzy result")
		}
		if typo[0].Match.Tier != models.MatchTierFuzzy {
			t.Errorf("typo hit tier = %q, want %q", typo[0].Match.Tier, models.MatchTierFuzzy)
		}
		// The fuzzy pass matched precisely because the literal substring is
		// absent, so an ILIKE-derived score would be zero. A real similarity
		// has to come back instead.
		if typo[0].Match.Confidence == nil {
			t.Fatal("fuzzy hit carries no confidence")
		}
		if *typo[0].Match.Confidence <= 0 {
			t.Errorf("fuzzy confidence = %.3f, want > 0 -- the ILIKE score was used instead of word_similarity",
				*typo[0].Match.Confidence)
		}
		t.Logf("typo   %-34s tier=%s confidence=%.3f",
			typo[0].FullAddress, typo[0].Match.Tier, *typo[0].Match.Confidence)
	}
}

// With no query there is nothing to have matched well or badly, so inventing a
// confidence would be worse than omitting it.
func TestFilterOnlySearchReportsNoConfidence(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	rows, _, err := svc.SearchAddresses(models.AddressSearchParams{City: "Columbus", Limit: 3})
	if err != nil {
		t.Fatalf("filter search: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected rows for a city filter")
	}

	if rows[0].Match == nil {
		t.Fatal("match metadata missing on a filtered search")
	}
	if rows[0].Match.Tier != models.MatchTierFilter {
		t.Errorf("tier = %q, want %q", rows[0].Match.Tier, models.MatchTierFilter)
	}
	if rows[0].Match.Confidence != nil {
		t.Errorf("confidence = %v, want absent -- there is no text to score against", *rows[0].Match.Confidence)
	}
}

// Confidence has to order results the way a human would rank them, or it is
// just a number riding along.
func TestConfidenceIsBoundedAndOrdered(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	rows, _, err := svc.SearchAddresses(models.AddressSearchParams{Query: "Barendt", Limit: 20})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	var prev float64 = 2
	for i, r := range rows {
		if r.Match == nil || r.Match.Confidence == nil {
			t.Fatalf("row %d has no confidence", i)
		}
		c := *r.Match.Confidence
		if c < 0 || c > 1 {
			t.Errorf("row %d confidence = %f, outside 0..1", i, c)
		}
		// Results are ordered by relevance, so confidence must not increase
		// as we walk down the list.
		if c > prev {
			t.Errorf("row %d confidence %.3f exceeds the row above it (%.3f); "+
				"confidence disagrees with the ordering", i, c, prev)
		}
		prev = c
	}
}
