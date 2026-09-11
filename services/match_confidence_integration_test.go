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

	// "barendtt" doubles a letter. The prefix index cannot match it, so only
	// the trigram fallback can. Deliberately not a transposition ("barnedt"):
	// those score 0.375, below the 0.6 threshold, and are documented as not
	// recovered -- using one would make the fuzzy assertions below unreachable.
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

	// Asserted, not guarded. An `if len(typo) > 0` here would let every fuzzy
	// assertion below silently vanish the moment the fallback stopped working,
	// which is the exact regression this test exists to catch.
	if len(typo) == 0 {
		t.Fatal("the trigram fallback returned nothing for a recoverable typo")
	}
	{
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

	if len(rows) == 0 {
		t.Fatal("no rows to check ordering against; the assertions below would pass vacuously")
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

// The relevance score this confidence is built on used to be the constant
// 150 x len(queryWords) for every row -- full_address contains every other
// scored column, so the top CASE arm always fired. Confidence was therefore
// always exactly 1.0, and the ordering it fed sorted nothing.
func TestPrefixConfidenceVariesBetweenRows(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	// The shared fixture seeds rows of one shape, so every hit is genuinely an
	// equally good match and an equal confidence is the right answer. To see
	// whether the score carries any signal at all, the rows have to differ in
	// match quality: same two query words, very different densities.
	if _, err := db.Exec(`
		INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		VALUES
		  ('rank-tight', '7', 'Barendt Road', '', 'Columbus', 'FRA', 'OH', '43004', 'Franklin',
		   ST_SetSRID(ST_MakePoint(-83.0, 40.0), 4326),
		   '7 Barendt Road, Columbus, OH 43004'),
		  ('rank-diffuse', '7', 'Barendt Road', '', 'Columbus', 'FRA', 'OH', '43004', 'Franklin',
		   ST_SetSRID(ST_MakePoint(-83.0, 40.0), 4326),
		   '7 Barendt Road Extension Seventeen Industrial Park Building Nine, Columbus, OH 43004')
	`); err != nil {
		t.Fatalf("seed contrasting rows: %v", err)
	}

	rows, _, err := svc.SearchAddresses(models.AddressSearchParams{
		Query: "Barendt Columbus", Limit: 100,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	confidence := map[string]float64{}
	for _, r := range rows {
		if r.Match == nil || r.Match.Confidence == nil {
			t.Fatal("missing confidence")
		}
		confidence[r.Hash] = *r.Match.Confidence
	}

	tight, okTight := confidence["rank-tight"]
	diffuse, okDiffuse := confidence["rank-diffuse"]
	if !okTight || !okDiffuse {
		t.Fatalf("seeded rows missing from results (tight=%t diffuse=%t)", okTight, okDiffuse)
	}

	t.Logf("tight   %.4f   diffuse %.4f", tight, diffuse)
	if tight == diffuse {
		t.Errorf("both rows report %.4f; the score carries no signal", tight)
	}
	if tight < diffuse {
		t.Errorf("the row where the query words sit close together (%.4f) scores below the diffuse one (%.4f)",
			tight, diffuse)
	}
}

// Punctuation used to break this: the predicate matched on the sanitized word
// while the score ILIKE'd the raw one, so a correct row scored zero.
func TestTrailingPunctuationDoesNotZeroConfidence(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	rows, _, err := svc.SearchAddresses(models.AddressSearchParams{Query: "Barendt.", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("a trailing full stop should not prevent the match")
	}
	if rows[0].Match == nil || rows[0].Match.Confidence == nil {
		t.Fatal("missing confidence")
	}
	if *rows[0].Match.Confidence <= 0 {
		t.Errorf("confidence = %.4f on a correctly matched row; the score is scoring a different string than the predicate matched",
			*rows[0].Match.Confidence)
	}
	t.Logf("query with trailing punctuation: confidence=%.4f", *rows[0].Match.Confidence)
}

// A query whose words are all too short adds no text predicate at all, so the
// rows returned matched nothing in particular. Reporting that as "filter" tells
// a caller they are looking at a deliberate structured result.
func TestDroppedQueryWordsAreNotReportedAsAFilterMatch(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	rows, _, err := svc.SearchAddresses(models.AddressSearchParams{Query: "I 5", Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) == 0 {
		t.Skip("no rows returned; nothing to label")
	}
	if rows[0].Match.Tier != models.MatchTierNone {
		t.Errorf("tier = %q, want %q -- a query that matched nothing must not look like a structured filter result",
			rows[0].Match.Tier, models.MatchTierNone)
	}
}
