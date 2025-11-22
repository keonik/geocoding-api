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
