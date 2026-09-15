package services

import (
	"strings"
	"testing"
)

// The question a checkout form or a CRM import asks is not "what matches this"
// but "is this real, and what should I store". Those are different, and search
// only answers the first.
func TestValidateConfirmsARealAddressAndCanonicalisesIt(t *testing.T) {
	db := setupBatchDB(t)

	// Lower case, abbreviated street type, no ZIP -- what someone actually
	// types into a form.
	result, err := ValidateAddress(db, "100 main st, columbus")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if !result.Verified {
		t.Fatalf("a real address was not verified; parsed as %+v", result.Parsed)
	}
	if result.Normalized == "" {
		t.Error("a verified address has no canonical form")
	}
	if result.Address == nil {
		t.Error("no matched record returned")
	}
	t.Logf("input %q -> %q", result.Input, result.Normalized)

	// The changes are the point: a caller has to be able to see what differs
	// and why, or they cannot tell a corrected typo from a wrong match.
	if len(result.Changes) == 0 {
		t.Error("no changes reported despite the input differing from the record")
	}
	reasons := map[string]bool{}
	for _, ch := range result.Changes {
		if ch.Reason == "" {
			t.Errorf("change to %s carries no reason", ch.Field)
		}
		reasons[ch.Field] = true
		t.Logf("  %s: %q -> %q (%s)", ch.Field, ch.From, ch.To, ch.Reason)
	}
	// The ZIP was absent and the record has one: that is a fill, and the most
	// useful thing this endpoint returns.
	if !reasons["postcode"] {
		t.Error("the missing postcode was not reported as filled in")
	}
}

// A well-formed address that does not exist is the case this endpoint exists
// for. Parsing it is not the same as confirming it.
func TestValidateRejectsAnAddressThatDoesNotExist(t *testing.T) {
	db := setupBatchDB(t)

	result, err := ValidateAddress(db, "14 Nonexistent Road, Columbus, OH 43215")
	if err != nil {
		t.Fatalf("validate should not error on a miss: %v", err)
	}
	if result.Verified {
		t.Error("an address that is not in the data was reported as verified")
	}
	// No canonical form is offered, because there is none -- echoing the input
	// back would present the caller's own guess to them as authority.
	if result.Normalized != "" {
		t.Errorf("a normalized form %q was offered for an unverified address", result.Normalized)
	}
	// The parse is still returned: "we read your street as the city" explains
	// a miss that an empty result does not.
	if result.Parsed == nil {
		t.Error("no parse returned, so the caller cannot see how their input was read")
	}
}

// Expanding an abbreviation and correcting a wrong value are different things
// to someone deciding whether to trust the result.
func TestValidateExplainsWhyEachFieldChanged(t *testing.T) {
	db := setupBatchDB(t)

	result, err := ValidateAddress(db, "100 main st, columbus")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !result.Verified {
		t.Skip("address not matched in this fixture")
	}

	for _, ch := range result.Changes {
		switch ch.Field {
		case "street":
			// "main st" against "Main Street" is an expansion, not a
			// disagreement.
			if strings.Contains(ch.Reason, "differs") {
				t.Errorf("street %q -> %q was reported as a disagreement rather than an expansion",
					ch.From, ch.To)
			}
		case "postcode":
			if ch.From != "" {
				t.Errorf("postcode reported as filled but From was %q", ch.From)
			}
		}
	}
}

func TestValidateRejectsEmptyInput(t *testing.T) {
	db := setupBatchDB(t)

	for _, in := range []string{"", "   ", "\t"} {
		if _, err := ValidateAddress(db, in); err == nil {
			t.Errorf("%q was accepted", in)
		}
	}
}
