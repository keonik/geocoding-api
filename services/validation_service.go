package services

import (
	"database/sql"
	"fmt"
	"strings"

	"geocoding-api/models"
	"geocoding-api/utils"
)

// FieldChange records one difference between what a caller sent and what the
// canonical record says, with why.
//
// The reason is the useful part. A validator that silently rewrites an address
// leaves the caller unable to tell a corrected typo from a wrong match, and so
// unable to decide whether to trust it.
type FieldChange struct {
	Field  string `json:"field"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// ValidationResult answers "is this a real address, and what is it really".
type ValidationResult struct {
	Input string `json:"input"`

	// Parsed is what the input was understood to mean, before any lookup. It is
	// returned even when nothing matches, because "we read your street as the
	// city" explains a no-match that an empty result does not.
	Parsed *utils.ParsedAddress `json:"parsed"`

	// Verified is whether this address exists in the data, as opposed to merely
	// being well-formed. The distinction matters: "14 Nonexistent Road,
	// Columbus, OH" parses perfectly and is not a place.
	Verified bool `json:"verified"`

	// Normalized is the canonical form, present only when verified. An address
	// that was not found has no canonical form to offer, and inventing one from
	// the input would be presenting the caller's guess back as authority.
	Normalized string `json:"normalized,omitempty"`

	Match   *models.AddressMatch `json:"match,omitempty"`
	Address *models.OhioAddress  `json:"address,omitempty"`

	Changes []FieldChange `json:"changes"`
}

// ValidateAddress parses an address, looks it up, and reports what differs.
//
// The pieces have been here all along -- utils.ParseAddressQuery, the
// abbreviation table, the search path -- but nothing put them together into the
// question an integration actually asks at a checkout form or a CRM import: is
// this real, and what should I store instead of what the user typed.
func ValidateAddress(db *sql.DB, input string) (*ValidationResult, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, fmt.Errorf("address is empty")
	}

	result := &ValidationResult{
		Input:   trimmed,
		Parsed:  utils.ParseAddressQuery(trimmed),
		Changes: []FieldChange{},
	}

	addresses, _, err := addressServiceFor(db).SearchAddresses(models.AddressSearchParams{
		Query: trimmed,
		Limit: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to look up the address: %w", err)
	}
	if len(addresses) == 0 {
		return result, nil
	}

	found := addresses[0]
	result.Verified = true
	result.Address = &found
	result.Match = found.Match
	result.Normalized = found.FullAddress
	result.Changes = diffAgainst(result.Parsed, &found)

	return result, nil
}

// diffAgainst compares what the caller wrote with the canonical record.
//
// Only fields the caller actually supplied are compared as corrections;
// anything they left out and the record has is reported as filled rather than
// changed, because those are different things to a caller deciding what to
// store.
func diffAgainst(parsed *utils.ParsedAddress, found *models.OhioAddress) []FieldChange {
	changes := []FieldChange{}

	compare := func(field, supplied, canonical string) {
		supplied = strings.TrimSpace(supplied)
		canonical = strings.TrimSpace(canonical)
		if canonical == "" {
			return
		}
		switch {
		case supplied == "":
			changes = append(changes, FieldChange{
				Field: field, From: "", To: canonical,
				Reason: "filled in from the matched address",
			})
		case strings.EqualFold(supplied, canonical):
			// Same value, different capitalisation. Worth reporting so a
			// caller storing the canonical form knows why it looks different,
			// but it is not a correction.
			if supplied != canonical {
				changes = append(changes, FieldChange{
					Field: field, From: supplied, To: canonical,
					Reason: "capitalisation normalised",
				})
			}
		case strings.EqualFold(utils.ExpandAddressQuery(supplied), canonical):
			changes = append(changes, FieldChange{
				Field: field, From: supplied, To: canonical,
				Reason: "abbreviation expanded",
			})
		default:
			changes = append(changes, FieldChange{
				Field: field, From: supplied, To: canonical,
				Reason: "differs from the matched address",
			})
		}
	}

	compare("house_number", parsed.HouseNumber, found.HouseNumber)
	compare("street", parsed.Street, found.Street)
	compare("city", parsed.City, found.City)
	compare("postcode", parsed.Zip, found.Postcode)
	compare("state", parsed.State, found.Region)

	return changes
}
