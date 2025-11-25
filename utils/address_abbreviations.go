package utils

import (
	"strings"
)

// streetAbbreviations maps common street type words to their possible variations
var streetAbbreviations = map[string][]string{
	"drive":      {"drive", "dr", "dr."},
	"road":       {"road", "rd", "rd."},
	"avenue":     {"avenue", "ave", "ave.", "av"},
	"street":     {"street", "st", "st."},
	"court":      {"court", "ct", "ct."},
	"lane":       {"lane", "ln", "ln."},
	"boulevard":  {"boulevard", "blvd", "blvd.", "bvd"},
	"place":      {"place", "pl", "pl."},
	"circle":     {"circle", "cir", "cir."},
	"trail":      {"trail", "trl", "trl.", "tr"},
	"parkway":    {"parkway", "pkwy", "pkwy."},
	"terrace":    {"terrace", "ter", "ter."},
	"way":        {"way", "wy", "wy."},
	"highway":    {"highway", "hwy", "hwy."},
	"pike":       {"pike", "pk", "pk."},
	"alley":      {"alley", "aly", "aly."},
	"annex":      {"annex", "anx", "anx."},
	"expressway": {"expressway", "expy", "expy."},
	"extension":  {"extension", "ext", "ext."},
	"freeway":    {"freeway", "fwy", "fwy."},
	"grove":      {"grove", "grv", "grv."},
	"heights":    {"heights", "hts", "hts."},
	"junction":   {"junction", "jct", "jct."},
	"landing":    {"landing", "lndg", "lndg."},
	"loop":       {"loop", "lp", "lp."},
	"point":      {"point", "pt", "pt."},
	"square":     {"square", "sq", "sq."},
	"trace":      {"trace", "trce", "trce."},
	"view":       {"view", "vw", "vw."},
	// Directional
	"north":     {"north", "n", "n."},
	"south":     {"south", "s", "s."},
	"east":      {"east", "e", "e."},
	"west":      {"west", "w", "w."},
	"northeast": {"northeast", "ne", "ne."},
	"northwest": {"northwest", "nw", "nw."},
	"southeast": {"southeast", "se", "se."},
	"southwest": {"southwest", "sw", "sw."},
}

// reverseAbbreviations maps abbreviations back to their full form
var reverseAbbreviations map[string]string

func init() {
	// Build reverse mapping for quick lookups
	reverseAbbreviations = make(map[string]string)
	for full, abbrevs := range streetAbbreviations {
		for _, abbrev := range abbrevs {
			reverseAbbreviations[strings.ToLower(abbrev)] = full
		}
	}
}

// ExpandAddressQuery expands street abbreviations in a search query
// Example: "7 westerfield dr" -> "7 westerfield drive"
func ExpandAddressQuery(query string) string {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return query
	}

	// Check if last word is an abbreviation
	lastWord := words[len(words)-1]
	// Remove trailing period if present
	lastWord = strings.TrimSuffix(lastWord, ".")

	if fullForm, exists := reverseAbbreviations[lastWord]; exists {
		words[len(words)-1] = fullForm
		return strings.Join(words, " ")
	}

	// Check second-to-last word (in case of "123 main st unit")
	if len(words) >= 2 {
		secondLast := strings.TrimSuffix(words[len(words)-2], ".")
		if fullForm, exists := reverseAbbreviations[secondLast]; exists {
			words[len(words)-2] = fullForm
			return strings.Join(words, " ")
		}
	}

	return query
}

// GetAbbreviationVariants returns all variants of a street type for pattern matching
// Example: "drive" -> ["drive", "dr", "dr."]
func GetAbbreviationVariants(word string) []string {
	word = strings.ToLower(strings.TrimSuffix(word, "."))
	
	// If it's already a full form, return its abbreviations
	if variants, exists := streetAbbreviations[word]; exists {
		return variants
	}
	
	// If it's an abbreviation, get the full form and return all variants
	if fullForm, exists := reverseAbbreviations[word]; exists {
		return streetAbbreviations[fullForm]
	}
	
	// Not a known street type, return as-is
	return []string{word}
}

// IsStreetType checks if a word is a known street type or abbreviation
func IsStreetType(word string) bool {
	word = strings.ToLower(strings.TrimSuffix(word, "."))
	_, isFull := streetAbbreviations[word]
	_, isAbbrev := reverseAbbreviations[word]
	return isFull || isAbbrev
}

// GetAddressQueryVariants returns all possible variants of an address query
// by expanding any street abbreviations to include both the abbreviation and full form.
// Example: "123 main dr" -> ["123 main dr", "123 main drive"]
// Example: "123 main drive" -> ["123 main drive", "123 main dr"]
func GetAddressQueryVariants(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return []string{query}
	}

	variants := make(map[string]bool)
	variants[strings.ToLower(query)] = true

	// Check last word for street type
	lastWord := strings.TrimSuffix(words[len(words)-1], ".")
	if allVariants := GetAbbreviationVariants(lastWord); len(allVariants) > 1 {
		// Build variants with each possible form
		for _, variant := range allVariants {
			newWords := make([]string, len(words))
			copy(newWords, words)
			newWords[len(newWords)-1] = variant
			variants[strings.Join(newWords, " ")] = true
		}
	}

	// Check second-to-last word (in case of "123 main st unit 5")
	if len(words) >= 2 {
		secondLast := strings.TrimSuffix(words[len(words)-2], ".")
		if allVariants := GetAbbreviationVariants(secondLast); len(allVariants) > 1 {
			for _, variant := range allVariants {
				newWords := make([]string, len(words))
				copy(newWords, words)
				newWords[len(newWords)-2] = variant
				variants[strings.Join(newWords, " ")] = true
			}
		}
	}

	// Check for directional prefixes (N, S, E, W, etc.) - often first or second word
	for i := 0; i < len(words) && i < 3; i++ {
		word := strings.TrimSuffix(words[i], ".")
		if allVariants := GetAbbreviationVariants(word); len(allVariants) > 1 {
			// Only expand if it's a directional
			fullForm, exists := reverseAbbreviations[word]
			if exists && isDirectional(fullForm) {
				for _, variant := range allVariants {
					newWords := make([]string, len(words))
					copy(newWords, words)
					newWords[i] = variant
					variants[strings.Join(newWords, " ")] = true
				}
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(variants))
	for v := range variants {
		result = append(result, v)
	}
	return result
}

// isDirectional checks if a word is a directional (N, S, E, W, etc.)
func isDirectional(word string) bool {
	directionals := map[string]bool{
		"north": true, "south": true, "east": true, "west": true,
		"northeast": true, "northwest": true, "southeast": true, "southwest": true,
	}
	return directionals[strings.ToLower(word)]
}
