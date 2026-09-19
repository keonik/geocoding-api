package database

// AddBoundariesForTest exposes migration 26 so other packages' integration
// tests build the real table rather than a copy that could drift from it.
func AddBoundariesForTest() error { return addBoundaries() }
