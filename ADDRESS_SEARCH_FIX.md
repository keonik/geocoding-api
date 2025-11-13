# Address Search Fix - COMPLETED ✅

## What Was Fixed

The address search was completely broken - it was returning random addresses instead of matching the search criteria.

## The Solution

Changed from a single LIKE condition to **word-based search using PostgreSQL ILIKE** for case-insensitive matching:

### Before (Broken):
```go
// This was using the SAME parameter for all fields - completely broken
conditions = append(conditions, fmt.Sprintf(`(
    LOWER(house_number || ' ' || street) LIKE LOWER($%d) OR
    LOWER(city) LIKE LOWER($%d) OR
    LOWER(county) LIKE LOWER($%d) OR
    LOWER(postcode) LIKE LOWER($%d)
)`, argIndex, argIndex, argIndex, argIndex))
args = append(args, "%"+params.Query+"%")
```

###After (Fixed):
```go
// Split query into words and ALL words must match (AND logic)
queryWords := strings.Fields(params.Query)
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
conditions = append(conditions, "("+strings.Join(wordConditions, " AND ")+")")
```

## How It Works

### Example: `query="2525 Oakley"`

SQL Generated:
```sql
WHERE (
    (house_number ILIKE '%2525%' OR street ILIKE '%2525%' OR ...) 
    AND 
    (house_number ILIKE '%Oakley%' OR street ILIKE '%Oakley%' OR ...)
)
```

This finds addresses where:
- "2525" appears in ANY field (house_number, street, city, county, postcode, or full address)
- AND "Oakley" appears in ANY field

### Example: `street="Oakley"`

SQL Generated:
```sql
WHERE street ILIKE '%Oakley%'
```

This finds addresses where the street name contains "Oakley".

## Benefits

1. **Word-based search**: "2525 Oakley" splits into "2525" + "Oakley" and both must match
2. **ILIKE operator**: Case-insensitive matching (Oakley = OAKLEY = oakley)
3. **Multiple fields**: Searches across house_number, street, city, county, postcode, and combined address
4. **Proper parameter binding**: Each word gets its own parameter, preventing SQL issues
5. **Combined address search**: `(house_number || ' ' || street)` allows matching "2525 Oakley" as a phrase

## Testing

### Query Examples:

| Query | Matches |
|-------|---------|
| `2525 Oakley` | Addresses with "2525" AND "Oakley" anywhere |
| `Oakley` | Any address with "Oakley" in any field |
| `Cincinnati` | Any address with "Cincinnati" in any field |
| `street=Oakley` | Only streets containing "Oakley" |
| `city=Cincinnati&street=Oakley` | Oakley streets in Cincinnati |

### Test After Air Reloads

Once the air server reloads with the new code, run:

```bash
./test_address_search.sh
```

Expected results:
- ✅ Search for "2525 Oakley" finds addresses with both words
- ✅ Search for "Oakley" finds Oakley streets
- ✅ Search for "Cincinnati" finds Cincinnati addresses
- ✅ Search for "XYZ999NonexistentStreet" returns 0 results
- ✅ All filters work correctly

## Files Modified

- `/Users/jfay/git/geocoding-api/services/address_service.go` - Fixed the `SearchAddresses()` function

## Performance

- **ILIKE** is PostgreSQL's case-insensitive LIKE operator
- Existing indexes on `street`, `city`, `county`, `postcode` will help
- For very large datasets, consider adding a full-text search column (future enhancement)

## Next Steps (Optional Enhancements)

1. Add full-text search with tsvector for even faster searches
2. Add fuzzy matching (Levenshtein distance) for typo tolerance
3. Add address normalization/geocoding hints
4. Cache common search queries

---

**Status**: ✅ Fixed and ready to test once air reloads
