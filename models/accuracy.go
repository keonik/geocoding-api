package models

// Accuracy says what a result's coordinates are the location *of*.
//
// Every result carries a latitude and longitude, and until now nothing said
// whether that point was the building, the middle of a ZIP code, or the middle
// of a city. Those differ by kilometres. A caller routing a delivery, drawing a
// pin or computing a distance needs to know which one it has, and the source
// table already decides it -- so it is stated rather than left to be guessed.
type Accuracy string

const (
	// AccuracyPoint is an address point from a county or statewide address
	// dataset: the location of that address, typically the structure or parcel.
	AccuracyPoint Accuracy = "point"

	// AccuracyPostalCentroid is the centre of a ZIP code's area. A ZIP can span
	// tens of kilometres, so this locates the postal area, not any address in it.
	AccuracyPostalCentroid Accuracy = "postal_centroid"

	// AccuracyLocalityCentroid is the representative point of a city or town.
	AccuracyLocalityCentroid Accuracy = "locality_centroid"
)
