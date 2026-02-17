package utils

import (
	"regexp"
	"strings"
)

// ParsedAddress represents components extracted from a free-form address query.
type ParsedAddress struct {
	HouseNumber string `json:"house_number,omitempty"`
	Street      string `json:"street,omitempty"`
	City        string `json:"city,omitempty"`
	State       string `json:"state,omitempty"`
	Zip         string `json:"zip,omitempty"`
	Raw         string `json:"raw"`
}

var (
	zipPattern         = regexp.MustCompile(`\b(\d{5})(?:-\d{4})?\s*$`)
	houseNumberPattern = regexp.MustCompile(`^(\d+[a-zA-Z]?(?:-\d+)?)\s+`)
)

// usStateCodes is the set of valid US state/territory 2-letter codes.
var usStateCodes = map[string]bool{
	"AL": true, "AK": true, "AZ": true, "AR": true, "CA": true,
	"CO": true, "CT": true, "DE": true, "FL": true, "GA": true,
	"HI": true, "ID": true, "IL": true, "IN": true, "IA": true,
	"KS": true, "KY": true, "LA": true, "ME": true, "MD": true,
	"MA": true, "MI": true, "MN": true, "MS": true, "MO": true,
	"MT": true, "NE": true, "NV": true, "NH": true, "NJ": true,
	"NM": true, "NY": true, "NC": true, "ND": true, "OH": true,
	"OK": true, "OR": true, "PA": true, "RI": true, "SC": true,
	"SD": true, "TN": true, "TX": true, "UT": true, "VT": true,
	"VA": true, "WA": true, "WV": true, "WI": true, "WY": true,
	"DC": true, "PR": true, "VI": true, "GU": true, "AS": true,
}

// IsUSStateCode checks whether a string is a valid US state/territory code.
func IsUSStateCode(s string) bool {
	return usStateCodes[strings.ToUpper(strings.TrimSpace(s))]
}

// ParseAddressQuery decomposes a free-form address string into structured components.
// It handles both comma-delimited and space-only formats.
func ParseAddressQuery(query string) *ParsedAddress {
	parsed := &ParsedAddress{Raw: query}
	query = strings.TrimSpace(query)
	if query == "" {
		return parsed
	}

	if strings.Contains(query, ",") {
		parseCommaDelimited(query, parsed)
	} else {
		parseSpaceDelimited(query, parsed)
	}

	// Trim all fields
	parsed.HouseNumber = strings.TrimSpace(parsed.HouseNumber)
	parsed.Street = strings.TrimSpace(parsed.Street)
	parsed.City = strings.TrimSpace(parsed.City)
	parsed.State = strings.TrimSpace(parsed.State)
	parsed.Zip = strings.TrimSpace(parsed.Zip)

	return parsed
}

// parseCommaDelimited handles "20 Overbrook Ct, Monroe, OH 45050" style input.
func parseCommaDelimited(query string, parsed *ParsedAddress) {
	parts := strings.Split(query, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	// Remove empty parts
	var cleaned []string
	for _, p := range parts {
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	parts = cleaned

	if len(parts) == 0 {
		return
	}

	// First part: house number + street
	extractHouseAndStreet(parts[0], parsed)

	// Process remaining parts from the end to find state/zip
	if len(parts) >= 2 {
		lastPart := parts[len(parts)-1]
		secondToLast := ""
		if len(parts) >= 3 {
			secondToLast = parts[len(parts)-2]
		}

		// Try to extract zip and state from last part(s)
		stateZipHandled := extractStateAndZip(lastPart, parsed)

		if stateZipHandled {
			// The part(s) before state/zip are the city
			if len(parts) == 2 {
				// "20 Overbrook Ct, Monroe OH 45050" - city is mixed with state/zip
				// City was already set in extractStateAndZip if it found leftover text
			} else if len(parts) >= 3 {
				// Check if second-to-last is state or city
				if parsed.State == "" && IsUSStateCode(secondToLast) {
					parsed.State = strings.ToUpper(secondToLast)
					if len(parts) >= 4 {
						parsed.City = parts[1]
					}
				} else {
					// Second-to-last is the city
					cityIdx := 1
					if len(parts) >= 4 {
						// Multiple middle parts - join them as city
						cityParts := parts[1 : len(parts)-1]
						parsed.City = strings.Join(cityParts, ", ")
					} else {
						parsed.City = parts[cityIdx]
					}
				}
			}
		} else {
			// Last part didn't have state/zip - might be "20 Main St, Monroe"
			if len(parts) == 2 {
				parsed.City = parts[1]
			} else if len(parts) >= 3 {
				// Try second-to-last as city, last as state/zip
				parsed.City = secondToLast
				extractStateAndZip(lastPart, parsed)
			}
		}
	}
}

// parseSpaceDelimited handles "20 Overbrook Ct Monroe OH 45050" style input.
func parseSpaceDelimited(query string, parsed *ParsedAddress) {
	remaining := query

	// 1. Extract zip from end
	if match := zipPattern.FindStringSubmatch(remaining); match != nil {
		parsed.Zip = match[1]
		remaining = strings.TrimSpace(remaining[:len(remaining)-len(match[0])])
	}

	// 2. Extract state from end (after removing zip)
	words := strings.Fields(remaining)
	if len(words) >= 2 {
		lastWord := words[len(words)-1]
		// Only treat as state code if it IS a state code AND either:
		// - it's not also a street type abbreviation (e.g., "OH" is unambiguous), OR
		// - we already found a zip code (strong signal this is state, not street type)
		// This prevents "Ct" from being misidentified as Connecticut when it means Court.
		if IsUSStateCode(lastWord) && (!IsStreetType(lastWord) || parsed.Zip != "") {
			parsed.State = strings.ToUpper(lastWord)
			remaining = strings.TrimSpace(strings.Join(words[:len(words)-1], " "))
		}
	}

	// 3. Extract house number from start
	if match := houseNumberPattern.FindStringSubmatch(remaining); match != nil {
		parsed.HouseNumber = match[1]
		remaining = strings.TrimSpace(remaining[len(match[0]):])
	}

	// 4. Split remaining into street vs city using street type as boundary
	splitStreetAndCity(remaining, parsed)
}

// extractHouseAndStreet pulls the house number from the front of a string,
// leaving the rest as the street name.
func extractHouseAndStreet(s string, parsed *ParsedAddress) {
	s = strings.TrimSpace(s)
	if match := houseNumberPattern.FindStringSubmatch(s); match != nil {
		parsed.HouseNumber = match[1]
		parsed.Street = strings.TrimSpace(s[len(match[0]):])
	} else {
		parsed.Street = s
	}
}

// extractStateAndZip tries to extract state code and/or zip from a string.
// Returns true if it found at least one of state or zip.
func extractStateAndZip(s string, parsed *ParsedAddress) bool {
	s = strings.TrimSpace(s)
	found := false

	// Try zip
	if match := zipPattern.FindStringSubmatch(s); match != nil {
		parsed.Zip = match[1]
		s = strings.TrimSpace(s[:len(s)-len(match[0])])
		found = true
	}

	// Try state from what remains
	words := strings.Fields(s)
	if len(words) >= 1 {
		lastWord := words[len(words)-1]
		if IsUSStateCode(lastWord) {
			parsed.State = strings.ToUpper(lastWord)
			// Anything before the state code is leftover (possibly city)
			if len(words) > 1 {
				leftover := strings.Join(words[:len(words)-1], " ")
				if parsed.City == "" {
					parsed.City = leftover
				}
			}
			found = true
		} else if !found && len(words) == 1 {
			// Single word, not a state code, not a zip - could be a city
			if parsed.City == "" {
				parsed.City = s
			}
		} else if !found {
			// Multiple words, no state/zip found - treat as city
			if parsed.City == "" {
				parsed.City = s
			}
		}
	}

	return found
}

// splitStreetAndCity splits a string like "Overbrook Ct Monroe" into street and city
// by finding the rightmost street type word as the boundary.
func splitStreetAndCity(s string, parsed *ParsedAddress) {
	words := strings.Fields(s)
	if len(words) == 0 {
		return
	}

	// Find the rightmost street type word as the boundary between street and city
	lastStreetTypeIdx := -1
	for i := len(words) - 1; i >= 0; i-- {
		if IsStreetType(words[i]) {
			lastStreetTypeIdx = i
			break
		}
	}

	if lastStreetTypeIdx >= 0 && lastStreetTypeIdx < len(words)-1 {
		// Street type found with words after it → split there
		parsed.Street = strings.Join(words[:lastStreetTypeIdx+1], " ")
		parsed.City = strings.Join(words[lastStreetTypeIdx+1:], " ")
	} else if lastStreetTypeIdx >= 0 {
		// Street type at the end → all street, no city
		parsed.Street = s
	} else if parsed.HouseNumber != "" {
		// No street type but has house number → likely a street name
		parsed.Street = s
	} else {
		// No street type, no house number → more likely a city/place name
		parsed.City = s
	}
}
