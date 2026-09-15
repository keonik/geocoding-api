package services

import (
	"database/sql"
	"fmt"

	"geocoding-api/database"
)

// MaxTileZoom is the deepest zoom served.
//
// County and state outlines carry no detail past this, so deeper tiles would be
// the same shapes re-cut into smaller squares -- more requests, more cache
// entries, no more information.
const MaxTileZoom = 14

// simplifiedBelowZoom is where the precomputed simplified geometry stops being
// good enough.
//
// Migration 19 stored outlines simplified to 0.0005 degrees, which is well
// inside a pixel while a whole state fits on screen and visible once a single
// county does. Above this zoom the full geometry is used instead.
const simplifiedBelowZoom = 10

// TileLayer is one of the layers that can be rendered.
type TileLayer struct {
	Name string
	// table and geometry columns for the two levels of detail.
	table         string
	simplifiedCol string
	fullCol       string
	// nameCol is carried into the tile so a renderer can label and pick out
	// features without a second request.
	nameCol string
}

// TileLayers is what /tiles can serve. Separate layers rather than one combined
// tile because the permission a caller needs differs -- counties and states are
// distinct scopes on every other endpoint, and a combined tile would have to
// demand both.
var TileLayers = map[string]TileLayer{
	"counties": {
		Name: "counties", table: "ohio_counties",
		simplifiedCol: "bounds_geometry_simplified", fullCol: "bounds_geometry",
		nameCol: "county_name",
	},
	"states": {
		Name: "states", table: "us_states",
		simplifiedCol: "geometry_simplified", fullCol: "geometry",
		nameCol: "state_abbr",
	},
}

// ValidTileCoordinates reports whether z/x/y names a real tile.
//
// At zoom z the grid is 2^z on a side, so anything outside that is not a tile
// that exists. Rejecting it keeps a malformed request from becoming an empty
// 200 that a client caches and retries forever.
func ValidTileCoordinates(z, x, y int) error {
	if z < 0 || z > MaxTileZoom {
		return fmt.Errorf("zoom %d is outside 0..%d", z, MaxTileZoom)
	}
	max := 1 << uint(z)
	if x < 0 || x >= max || y < 0 || y >= max {
		return fmt.Errorf("tile (%d, %d) does not exist at zoom %d, where the grid is %dx%d", x, y, z, max, max)
	}
	return nil
}

// RenderTile returns a Mapbox Vector Tile, or nil when the tile is empty.
//
// The geometry is already in the database and already simplified; ST_AsMVT does
// the encoding, so nothing is serialised through Go. A tile with no features
// comes back nil rather than as an empty encoding, so the handler can answer
// 204 and save the client parsing a tile with nothing in it.
func RenderTile(db *sql.DB, layer TileLayer, z, x, y int) ([]byte, error) {
	if db == nil {
		db = database.DB
	}

	// The simplified column is only worth using where the detail it drops is
	// smaller than a pixel.
	geomCol := layer.simplifiedCol
	if z >= simplifiedBelowZoom {
		geomCol = layer.fullCol
	}

	// COALESCE because the simplified columns are generated and can be null on
	// a row whose source geometry was null; falling back keeps a gap in one row
	// from blanking the whole tile.
	query := fmt.Sprintf(`
		WITH bounds AS (
			SELECT ST_TileEnvelope($1, $2, $3) AS geom
		),
		features AS (
			SELECT
				ST_AsMVTGeom(
					ST_Transform(COALESCE(t.%s, t.%s), 3857),
					bounds.geom
				) AS geom,
				t.%s AS name
			FROM %s t, bounds
			WHERE COALESCE(t.%s, t.%s) IS NOT NULL
			  AND ST_Transform(COALESCE(t.%s, t.%s), 3857) && bounds.geom
		)
		SELECT ST_AsMVT(features.*, $4)
		FROM features
		WHERE features.geom IS NOT NULL
	`, geomCol, layer.fullCol, layer.nameCol, layer.table,
		geomCol, layer.fullCol, geomCol, layer.fullCol)

	var tile []byte
	if err := db.QueryRow(query, z, x, y, layer.Name).Scan(&tile); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to render tile: %w", err)
	}

	// ST_AsMVT over no rows returns an empty encoding rather than null.
	if len(tile) == 0 {
		return nil, nil
	}
	return tile, nil
}
