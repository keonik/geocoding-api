package services

import "fmt"

// DefaultBoundaryTolerance is the Douglas-Peucker distance, in SRID 4326
// degrees (~55 m), that migration 19 bakes into the precomputed
// geometry_simplified / bounds_geometry_simplified columns. A request at this
// tolerance is a plain column read; anything else has to simplify on the fly.
const DefaultBoundaryTolerance = 0.0005

// boundaryGeometrySQL picks the cheapest expression for the requested
// tolerance and reports whether that expression references a tolerance
// placeholder.
//
// The caller must append the tolerance argument if and only if this returns
// true. Passing an argument the statement never mentions makes Postgres fail
// to infer its type ("could not determine data type of parameter $n") rather
// than ignoring it.
func boundaryGeometrySQL(rawColumn, simplifiedColumn string, tolerance float64, tolPlaceholder int) (string, bool) {
	switch {
	case tolerance == 0:
		// Explicit opt-out: full resolution, the expensive path.
		return rawColumn, false
	case tolerance == DefaultBoundaryTolerance:
		// The common case, already materialised.
		return simplifiedColumn, false
	default:
		return fmt.Sprintf("ST_SimplifyPreserveTopology(%s, $%d)", rawColumn, tolPlaceholder), true
	}
}
