# Address Search Bug Report

## Problem
The address search functionality is completely broken. When searching for addresses, it returns incorrect results:

- Searching for "2525 Oakley" returns addresses from Adams County (LANDSBROOK, BLUE CREEK, etc.)
- Searching with `street=OAKLEY` returns LANDSBROOK addresses
- Even with specific filters, wrong addresses are returned
- Total count shows 5,999,811 for all searches (the entire database)

## Root Cause
The SQL WHERE clause is being constructed incorrectly. While the code builds the conditions array properly, there's likely an issue with how the arguments are being passed to the SQL query or how the LIKE patterns are being applied.

## Test Results
Run `./test_address_search.sh` to see the failures:

```bash
3. Testing search by query '2525 Oakley'... OK - Found 50 addresses
   Sample result:
   135 LANDSBROOK,   # WRONG - should find Oakley addresses
4. Testing search by street 'Oakley'... OK - Found 50 addresses  # Returns LANDSBROOK
5. Testing search by city 'Cincinnati'... OK - Found 50 addresses
6. Testing search by county 'Hamilton'... OK - Found 50 addresses
7. Testing search by postcode '45209'... OK - Found 50 addresses
8. Testing combined search (city=Cincinnati, street=Oakley)... OK - Found 50 addresses
9. Testing proximity search (39.1031, -84.5120, 2km radius)... OK - Found 50 addresses
10. Testing non-existent address... FAILED - Expected 0 results, got 50  # WRONG
```

Every search returns exactly 50 results and total=5999811, regardless of the search criteria.

## Suspected Issues in `services/address_service.go`

### Issue 1: Query Parameter Binding
The code uses numbered placeholders (`$1`, `$2`, etc.) but the way conditions are built and args are appended might be causing parameter mismatch.

### Issue 2: WHERE Clause Not Applied
Even though the code builds a WHERE clause:
```go
whereClause := ""
if len(conditions) > 0 {
    whereClause = "WHERE " + strings.Join(conditions, " AND ")
}
```

The query might not be applying it correctly, or the args array isn't matching the placeholders.

### Issue 3: Default Behavior
When no conditions match or filters fail, the query returns all addresses (limited to 50) instead of returning an empty result set.

## How to Debug

1. Add logging to see the actual SQL query being executed:
```go
log.Printf("Full Query: %s", fullQuery)
log.Printf("Args: %v", args)
```

2. Check if `conditions` array is being populated:
```go
log.Printf("Conditions: %v", conditions)
log.Printf("WHERE clause: %s", whereClause)
```

3. Verify the parameter indices are correct

## Recommended Fix

The SQL query construction needs to be reviewed. The issue is likely one of:

1. **Parameter index mismatch**: The `argIndex` variable might not be tracking correctly when building multiple conditions
2. **SQL injection vulnerability**: The way conditions are joined might be allowing SQL bypass
3. **Query execution**: The args might not be passed correctly to `db.Query()`

## Immediate Workaround

Until the search is fixed, users should:
1. Use the semantic search endpoint `/api/v1/addresses/semantic` if available
2. Use very specific county + city + postcode combinations
3. Avoid using the `query` parameter alone

## Files to Fix
- `services/address_service.go` - `SearchAddresses()` function (lines 20-154)
- Consider adding integration tests that actually verify search results match the criteria

## Test Script
Run the integration test script:
```bash
chmod +x test_address_search.sh
./test_address_search.sh
```

This will create a test user, API key, and run various search scenarios to verify the fix.
