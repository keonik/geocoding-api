package main

import (
	"fmt"
	"strings"
)

func main() {
	// Test the query parsing logic
	queries := []string{
		"2525 Oakley",
		"Oakley",
		"Cincinnati",
		"XYZ999NonexistentStreet",
	}

	for _, query := range queries {
		queryWords := strings.Fields(query)
		var wordConditions []string
		var args []interface{}
		argIndex := 1

		for _, word := range queryWords {
			wordConditions = append(wordConditions, fmt.Sprintf(`(
					house_number ILIKE $%d OR
					street ILIKE $%d OR
					city ILIKE $%d OR
					county ILIKE $%d OR
					postcode ILIKE $%d OR
					(house_number || ' ' || street) ILIKE $%d
				)`, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex))
			args = append(args, "%"+word+"%")
			argIndex++
		}

		whereClause := ""
		if len(wordConditions) > 0 {
			whereClause = "WHERE (" + strings.Join(wordConditions, " AND ") + ")"
		}

		fmt.Printf("\nQuery: %q\n", query)
		fmt.Printf("Words: %v\n", queryWords)
		fmt.Printf("WHERE: %s\n", whereClause)
		fmt.Printf("Args: %v\n", args)
	}
}
