package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"geocoding-api/database"

	shp "github.com/jonas-p/go-shp"
)

// BoundaryLayer is one Census TIGER/Line polygon layer that enrichment can
// answer from.
type BoundaryLayer struct {
	// Name is how the layer is addressed in the API and stored in the table.
	Name string
	// Group is the enrichment field that returns it -- the names Geocodio
	// uses, so a caller moving from there asks for the same things.
	Group string
	// Dir and Suffix locate the per-state file:
	// TIGER2025/<Dir>/tl_2025_<statefips>_<Suffix>.zip
	Dir, Suffix string
	// URL is the whole address instead, for a layer that is not TIGER. A
	// layer with one is national: it covers the country in a single file, is
	// loaded once rather than per state, and is recorded against
	// NationalScope.
	URL string
	// IDColumn and NameColumn name the DBF columns holding the identifier and
	// the label. Empty means the TIGER pair, GEOID and NAMELSAD.
	IDColumn, NameColumn string
	// Attrs maps DBF columns to the attribute names returned for this layer.
	// Only these are stored: TIGER ships a dozen columns per layer, most of
	// them feature-class codes no caller asked for.
	Attrs map[string]string
}

// BoundaryLayers is every layer, in the order enrichment reports them.
//
// All are per-state files. A layer a state does not have -- Ohio has only
// unified school districts, so no elementary or secondary file exists -- is
// recorded as absent rather than failed.
var BoundaryLayers = []BoundaryLayer{
	{Name: "tract", Group: "census", Dir: "TRACT", Suffix: "tract",
		Attrs: map[string]string{"COUNTYFP": "county_fips", "TRACTCE": "tract_code"}},
	{Name: "block_group", Group: "census", Dir: "BG", Suffix: "bg",
		Attrs: map[string]string{"COUNTYFP": "county_fips", "TRACTCE": "tract_code", "BLKGRPCE": "block_group"}},
	// Blocks are 2020-vintage, as the Census publishes them, and large: Ohio
	// alone is 147 MB. Load them only where block-level answers are needed.
	{Name: "block", Group: "census", Dir: "TABBLOCK20", Suffix: "tabblock20",
		Attrs: map[string]string{"COUNTYFP20": "county_fips", "TRACTCE20": "tract_code", "BLOCKCE20": "block_code"}},
	{Name: "congressional_district", Group: "cd", Dir: "CD", Suffix: "cd119",
		Attrs: map[string]string{"CD119FP": "district", "CDSESSN": "congress"}},
	{Name: "state_senate", Group: "stateleg", Dir: "SLDU", Suffix: "sldu",
		Attrs: map[string]string{"SLDUST": "district", "LSY": "legislative_year"}},
	{Name: "state_house", Group: "stateleg", Dir: "SLDL", Suffix: "sldl",
		Attrs: map[string]string{"SLDLST": "district", "LSY": "legislative_year"}},
	{Name: "school_unified", Group: "school", Dir: "UNSD", Suffix: "unsd",
		Attrs: map[string]string{"UNSDLEA": "lea_code", "LOGRADE": "lowest_grade", "HIGRADE": "highest_grade"}},
	{Name: "school_elementary", Group: "school", Dir: "ELSD", Suffix: "elsd",
		Attrs: map[string]string{"ELSDLEA": "lea_code", "LOGRADE": "lowest_grade", "HIGRADE": "highest_grade"}},
	{Name: "school_secondary", Group: "school", Dir: "SCSD", Suffix: "scsd",
		Attrs: map[string]string{"SCSDLEA": "lea_code", "LOGRADE": "lowest_grade", "HIGRADE": "highest_grade"}},
	{Name: "place", Group: "place", Dir: "PLACE", Suffix: "place",
		Attrs: map[string]string{"PLACEFP": "place_fips", "LSAD": "lsad"}},
	// Not a Census layer: the IANA zones, as polygons, from
	// timezone-boundary-builder. One file covers the world, so only the zones
	// that touch a loaded state are kept -- see replaceBoundaries.
	//
	// This is the release that keeps every zone distinct that has differed
	// since 1970, rather than the "now" file that merges zones currently
	// agreeing: America/Indiana/Indianapolis stays itself rather than
	// becoming America/New_York.
	{Name: "timezone", Group: "timezone", URL: timezoneBoundaryURL,
		IDColumn: "tzid", NameColumn: "tzid"},
}

// timezoneBoundaryURL is pinned to a release: the project publishes several
// times a year as the IANA database changes, and a load should not silently
// change which vintage it fetched. TimezoneBoundarySource names it in
// responses, as ODbL attribution requires.
const (
	timezoneBoundaryRelease = "2026d"
	timezoneBoundaryURL     = "https://github.com/evansiroky/timezone-boundary-builder/releases/download/" +
		timezoneBoundaryRelease + "/timezones.shapefile.zip"

	// TimezoneBoundarySource is the timezone_source of a zone read from those
	// polygons, and its attribution: the data is ODbL, from OpenStreetMap.
	TimezoneBoundarySource = "timezone-boundary-builder " + timezoneBoundaryRelease + " (ODbL)"

	// TimezoneZIPSource is the timezone_source of the older, approximate
	// answer: the zone of the nearest ZIP code centroid.
	TimezoneZIPSource = "zip_centroid"
)

// NationalScope stands in for a state FIPS code on a layer that is not loaded
// per state. "00" is not a state code, so it cannot collide with one.
const NationalScope = "00"

// BoundaryLayerByName finds a layer, or reports that there is none.
func BoundaryLayerByName(name string) (BoundaryLayer, bool) {
	for _, l := range BoundaryLayers {
		if l.Name == name {
			return l, true
		}
	}
	return BoundaryLayer{}, false
}

// tigerBaseURL is a variable so tests can serve their own shapefiles.
var tigerBaseURL = "https://www2.census.gov/geo/tiger/TIGER2025"

// sourceURL is where a layer's file for a state lives.
func sourceURL(layer BoundaryLayer, stateFIPS string) string {
	if layer.URL != "" {
		return layer.URL
	}
	return fmt.Sprintf("%s/%s/tl_2025_%s_%s.zip", tigerBaseURL, layer.Dir, stateFIPS, layer.Suffix)
}

// IsNational reports whether one file covers the country, so the layer is
// loaded once rather than per state.
func (l BoundaryLayer) IsNational() bool { return l.URL != "" }

// Scope is the state a layer is loaded against: the state itself for a Census
// layer, NationalScope for a national one.
func (l BoundaryLayer) Scope(stateFIPS string) string {
	if l.IsNational() {
		return NationalScope
	}
	return stateFIPS
}

// staleLoadAfter is how long a load may sit in 'loading' before another is
// allowed to replace it. A crash mid-load leaves the row behind, and without
// this the layer could never be reloaded for that state.
const staleLoadAfter = time.Hour

// ErrBoundaryLoadInProgress is returned when the same layer and state are
// already loading.
var ErrBoundaryLoadInProgress = errors.New("this layer is already loading for that state")

var stateFIPSPattern = regexp.MustCompile(`^[0-9]{2}$`)

// ResolveStateFIPS accepts a FIPS code ("39") or a postal abbreviation
// ("OH") and returns the FIPS code, checked against us_states.
func ResolveStateFIPS(db *sql.DB, state string) (string, error) {
	state = strings.ToUpper(strings.TrimSpace(state))
	var fips string
	err := db.QueryRow(
		`SELECT state_fips FROM us_states WHERE state_fips = $1 OR state_abbr = $1`, state,
	).Scan(&fips)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("unknown state %q", state)
	}
	if err != nil {
		return "", fmt.Errorf("failed to resolve state: %w", err)
	}
	return fips, nil
}

// DefaultBoundaryLayers is what a load without a layer list loads: every
// Census layer except blocks, which are an order of magnitude larger than the
// rest (Ohio: 147 MB, 276,000 polygons) and worth loading only where
// block-level answers are needed.
//
// The timezone layer is left out for a different reason: it is national, so
// loading it once is enough, and repeating it for each state would re-fetch
// 80 MB to write the same rows.
func DefaultBoundaryLayers() []BoundaryLayer {
	var layers []BoundaryLayer
	for _, l := range BoundaryLayers {
		if l.Name != "block" && !l.IsNational() {
			layers = append(layers, l)
		}
	}
	return layers
}

// BoundaryClaim is a held claim on loading one layer for one state.
//
// The claim carries an id, and every write the load makes about itself is
// conditioned on it. A load that outlives staleLoadAfter can have its claim
// taken over; when it finally finishes, it must not write its outcome over
// the load that replaced it.
type BoundaryClaim struct {
	Layer     BoundaryLayer
	StateFIPS string
	id        string
}

// BeginBoundaryLoad claims a layer and state for loading, so two requests
// cannot load the same file into the same rows at once. The caller then runs
// Load, which releases the claim whatever happens.
func BeginBoundaryLoad(db *sql.DB, layer BoundaryLayer, stateFIPS string) (*BoundaryClaim, error) {
	if err := database.RequireSchemaVersion(db, database.SchemaVersionBoundaryGeoIDs); err != nil {
		return nil, err
	}
	stateFIPS = layer.Scope(stateFIPS)
	if !stateFIPSPattern.MatchString(stateFIPS) {
		return nil, fmt.Errorf("state FIPS %q is not two digits", stateFIPS)
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("failed to generate a claim id: %w", err)
	}
	claim := &BoundaryClaim{Layer: layer, StateFIPS: stateFIPS, id: hex.EncodeToString(idBytes)}

	res, err := db.Exec(`
		INSERT INTO boundary_loads (layer, state_fips, status, claim_id, source_url, started_at)
		VALUES ($1, $2, 'loading', $3, $4, NOW())
		ON CONFLICT (layer, state_fips) DO UPDATE
		SET status = 'loading', claim_id = EXCLUDED.claim_id, source_url = EXCLUDED.source_url,
		    error = NULL, started_at = NOW(), finished_at = NULL
		WHERE boundary_loads.status <> 'loading'
		   OR boundary_loads.started_at < NOW() - make_interval(secs => $5)
	`, layer.Name, stateFIPS, claim.id, sourceURL(layer, stateFIPS), staleLoadAfter.Seconds())
	if err != nil {
		return nil, fmt.Errorf("failed to record the boundary load: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrBoundaryLoadInProgress
	}
	return claim, nil
}

// Load downloads the state's TIGER file for the layer and replaces that
// state's rows with it.
//
// The replace is one transaction: a lookup during the load sees the old
// polygons or the new ones, never a state half-loaded, and a load that fails
// leaves the previous one serving. The outcome is written to boundary_loads
// whatever happens -- including a panic on a malformed file -- since the load
// runs after the request that started it has returned.
func (c *BoundaryClaim) Load(ctx context.Context, db *sql.DB) (int, error) {
	n, status, err := c.load(ctx, db)

	errText := sql.NullString{}
	if err != nil {
		status = "failed"
		errText = sql.NullString{String: err.Error(), Valid: true}
	}
	res, uerr := db.Exec(`
		UPDATE boundary_loads
		SET status = $4::text, error = $6, finished_at = NOW(), claim_id = NULL,
		    -- A failure rolled back, so whatever was serving still is.
		    available = CASE WHEN $4::text IN ('loaded', 'absent') THEN true ELSE available END,
		    features = CASE WHEN $4::text IN ('loaded', 'absent') THEN $5 ELSE features END
		WHERE layer = $1 AND state_fips = $2 AND claim_id = $3
	`, c.Layer.Name, c.StateFIPS, c.id, status, n, errText)
	if uerr != nil && err == nil {
		err = fmt.Errorf("loaded, but failed to record it: %w", uerr)
	}
	if uerr == nil {
		if rows, _ := res.RowsAffected(); rows == 0 && err == nil {
			err = errors.New("loaded, but the claim was taken over before it finished; the newer load's outcome stands")
		}
	}
	return n, err
}

// load does the work; Load records it.
func (c *BoundaryClaim) load(ctx context.Context, db *sql.DB) (n int, status string, err error) {
	defer func() {
		if r := recover(); r != nil {
			n, status, err = 0, "", fmt.Errorf("loading %s for state %s panicked: %v", c.Layer.Name, c.StateFIPS, r)
		}
	}()

	url := sourceURL(c.Layer, c.StateFIPS)
	path, found, err := downloadTigerFile(ctx, url)
	if err != nil {
		return 0, "", err
	}
	if !found {
		// A 404 means the state has no such layer -- unless it had one a
		// moment ago. Rows already loaded mean the file moved, not that the
		// districts vanished, and replacing them with a trusted "none" is the
		// exact confusion boundary_loads exists to prevent.
		var loaded int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM boundaries WHERE layer = $1 AND state_fips = $2`,
			c.Layer.Name, c.StateFIPS).Scan(&loaded); err != nil {
			return 0, "", fmt.Errorf("failed to count loaded %s: %w", c.Layer.Name, err)
		}
		if loaded > 0 {
			return 0, "", fmt.Errorf("census.gov has no file at %s; keeping the %d %s already loaded", url, loaded, c.Layer.Name)
		}
		return 0, "absent", nil
	}
	defer os.Remove(path)

	n, err = replaceBoundaries(ctx, db, c.Layer, c.StateFIPS, path)
	if err != nil {
		return 0, "", err
	}
	return n, "loaded", nil
}

// tigerClient bounds each stage of a download. The load's context bounds the
// whole, but a server that accepts the connection and never answers would
// otherwise hold the claim for the full half hour.
var tigerClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout:   30 * time.Second,
	ResponseHeaderTimeout: time.Minute,
}}

// downloadTigerFile fetches a zip to a temporary file. found is false when the
// Census has no such file, which for TIGER means the layer does not exist for
// that state.
func downloadTigerFile(ctx context.Context, url string) (path string, found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false, fmt.Errorf("failed to build a request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "geocoding-api boundary loader")

	resp, err := tigerClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("failed to download %s: HTTP %d", url, resp.StatusCode)
	}

	f, err := os.CreateTemp("", "tiger-*.zip")
	if err != nil {
		return "", false, fmt.Errorf("failed to create a temporary file: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", false, fmt.Errorf("failed to download %s: %w", url, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", false, fmt.Errorf("failed to write %s: %w", url, err)
	}
	return f.Name(), true, nil
}

// A batch is flushed at whichever limit it reaches first. Block groups average
// ~2 KB of WKT, so 200 of them is a few hundred KB; 200 congressional or state
// senate districts with their coastlines would be tens of MB, which the byte
// limit stops.
const (
	boundaryInsertBatch      = 200
	boundaryInsertBatchBytes = 8 << 20
)

// replaceBoundaries reads a shapefile zip and swaps it in for the state's rows.
func replaceBoundaries(ctx context.Context, db *sql.DB, layer BoundaryLayer, stateFIPS, zipPath string) (int, error) {
	z, err := shp.OpenZip(zipPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open %s shapefile: %w", layer.Name, err)
	}
	defer z.Close()

	cols := map[string]int{}
	for i, f := range z.Fields() {
		cols[f.String()] = i
	}
	// Block files suffix every column with the vintage: GEOID20, NAME20.
	col := func(name string) (int, bool) {
		if i, ok := cols[name]; ok {
			return i, true
		}
		i, ok := cols[name+"20"]
		return i, ok
	}
	idColumn, nameColumn := layer.IDColumn, layer.NameColumn
	if idColumn == "" {
		idColumn = "GEOID"
	}
	geoidCol, ok := col(idColumn)
	if !ok {
		return 0, fmt.Errorf("%s shapefile has no %s column", layer.Name, idColumn)
	}
	nameCol, ok := col(nameColumn)
	if !ok && nameColumn == "" {
		// TIGER labels a feature NAMELSAD ("Census Tract 40.02"); a few
		// layers carry only the bare NAME.
		if nameCol, ok = col("NAMELSAD"); !ok {
			nameCol, ok = col("NAME")
		}
	}
	if !ok {
		return 0, fmt.Errorf("%s shapefile has no name column", layer.Name)
	}

	// A national file covers the world, and only what touches a loaded state
	// is wanted. The bounds of each state cheaply reject most of it before a
	// polygon is ever built; ST_Intersects settles the rest on the way in.
	var stateBounds []shp.Box
	if layer.IsNational() {
		stateBounds, err = loadStateBounds(ctx, db)
		if err != nil {
			return 0, err
		}
		if len(stateBounds) == 0 {
			return 0, errors.New("no state boundaries are loaded, so there is nothing to select zones against")
		}
	}

	// DBF pads fixed-width fields. The Census pads with spaces, which the
	// reader trims; other producers pad with NULs, which it does not, and
	// which Postgres refuses in text.
	attr := func(i int) string { return strings.Trim(z.Attribute(i), " \x00") }

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin boundary load: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM boundaries WHERE layer = $1 AND state_fips = $2`, layer.Name, stateFIPS); err != nil {
		return 0, fmt.Errorf("failed to clear %s for state %s: %w", layer.Name, stateFIPS, err)
	}

	national := layer.IsNational()
	var batch []interface{}
	var batchBytes int
	var firstGEOID, lastGEOID string
	n := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		values := make([]string, len(batch)/4)
		for i := range values {
			b := i*4 + 2 // $1 and $2 are layer and state
			// ST_BuildArea assembles the rings, working out which are holes:
			// shapefiles store a polygon as a flat list of rings, and sorting
			// them into outers and inners is exactly what it does.
			values[i] = fmt.Sprintf("($1, $%d, $2, $%d, $%d::jsonb, ST_Multi(ST_BuildArea(ST_GeomFromText($%d, 4326))))",
				b+1, b+2, b+3, b+4)
		}
		args := append([]interface{}{layer.Name, stateFIPS}, batch...)
		insert := `INSERT INTO boundaries (layer, geoid, state_fips, name, attrs, geom) VALUES ` + strings.Join(values, ", ")
		if national {
			insert = `INSERT INTO boundaries (layer, geoid, state_fips, name, attrs, geom)
				SELECT v.* FROM (VALUES ` + strings.Join(values, ", ") + `) AS v(layer, geoid, state_fips, name, attrs, geom)
				WHERE EXISTS (SELECT 1 FROM us_states s
				              WHERE s.geometry IS NOT NULL AND ST_Intersects(s.geometry, v.geom))`
		}
		if _, err := tx.ExecContext(ctx, insert, args...); err != nil {
			// A feature whose rings form no area fails NOT NULL on geom; the
			// range is what finds it in a file of thousands.
			return fmt.Errorf("failed to insert %s boundaries %s..%s: %w", layer.Name, firstGEOID, lastGEOID, err)
		}
		batch, batchBytes = batch[:0], 0
		return nil
	}

	for z.Next() {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("stopped loading %s: %w", layer.Name, err)
		}
		_, shape := z.Shape()
		poly, ok := shape.(*shp.Polygon)
		if !ok {
			return 0, fmt.Errorf("%s shapefile holds %T, not polygons", layer.Name, shape)
		}
		if national && !nearAnyState(poly.Box, stateBounds) {
			continue
		}
		attrs := map[string]string{}
		for dbf, key := range layer.Attrs {
			if i, ok := cols[dbf]; ok {
				attrs[key] = attr(i)
			}
		}
		attrJSON, err := json.Marshal(attrs)
		if err != nil {
			return 0, fmt.Errorf("failed to encode %s attributes: %w", layer.Name, err)
		}
		geoid := attr(geoidCol)
		wkt, err := ringsWKT(poly)
		if err != nil {
			return 0, fmt.Errorf("%s %s: %w", layer.Name, geoid, err)
		}
		if len(batch) == 0 {
			firstGEOID = geoid
		}
		lastGEOID = geoid
		batch = append(batch, geoid, attr(nameCol), string(attrJSON), wkt)
		batchBytes += len(wkt)
		n++
		if len(batch)/4 >= boundaryInsertBatch || batchBytes >= boundaryInsertBatchBytes {
			if err := flush(); err != nil {
				return 0, err
			}
		}
	}
	if err := z.Err(); err != nil {
		return 0, fmt.Errorf("failed to read %s shapefile: %w", layer.Name, err)
	}
	if err := flush(); err != nil {
		return 0, err
	}
	// What was kept, which for a national layer is a fraction of what was
	// read: 419 zones cover the world, a few dozen touch the United States.
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM boundaries WHERE layer = $1 AND state_fips = $2`, layer.Name, stateFIPS).Scan(&n); err != nil {
		return 0, fmt.Errorf("failed to count %s boundaries: %w", layer.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit %s boundaries: %w", layer.Name, err)
	}
	return n, nil
}

// loadStateBounds reads the bounding box of every state with a boundary.
func loadStateBounds(ctx context.Context, db *sql.DB) ([]shp.Box, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT ST_XMin(geometry), ST_YMin(geometry), ST_XMax(geometry), ST_YMax(geometry)
		FROM us_states WHERE geometry IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("failed to read state bounds: %w", err)
	}
	defer rows.Close()
	var boxes []shp.Box
	for rows.Next() {
		var b shp.Box
		if err := rows.Scan(&b.MinX, &b.MinY, &b.MaxX, &b.MaxY); err != nil {
			return nil, fmt.Errorf("failed to read state bounds: %w", err)
		}
		boxes = append(boxes, b)
	}
	return boxes, rows.Err()
}

// nearAnyState reports whether a feature's bounds overlap any state's.
//
// Only a filter, and a loose one: Alaska reaches past the antimeridian, so its
// bounds span most of the globe and admit zones on the far side of it. What
// gets through is settled exactly by ST_Intersects.
func nearAnyState(b shp.Box, states []shp.Box) bool {
	for _, s := range states {
		if b.MinX <= s.MaxX && b.MaxX >= s.MinX && b.MinY <= s.MaxY && b.MaxY >= s.MinY {
			return true
		}
	}
	return false
}

// ringsWKT writes a shapefile polygon's rings as a MULTILINESTRING, for
// ST_BuildArea to assemble.
//
// The part offsets come from the file and are checked: a corrupt one would
// otherwise index past the points and panic.
func ringsWKT(p *shp.Polygon) (string, error) {
	if int(p.NumParts) != len(p.Parts) || len(p.Parts) == 0 {
		return "", fmt.Errorf("polygon declares %d parts but has %d", p.NumParts, len(p.Parts))
	}
	var b strings.Builder
	b.WriteString("MULTILINESTRING(")
	rings := 0
	for part := 0; part < len(p.Parts); part++ {
		start := int(p.Parts[part])
		end := len(p.Points)
		if part+1 < len(p.Parts) {
			end = int(p.Parts[part+1])
		}
		if start < 0 || start > end || end > len(p.Points) {
			return "", fmt.Errorf("polygon ring %d spans points %d..%d of %d", part, start, end, len(p.Points))
		}
		// A ring needs four points to close around an area. A shorter one
		// encloses nothing, and ST_BuildArea would ignore it anyway.
		if end-start < 4 {
			continue
		}
		if rings > 0 {
			b.WriteByte(',')
		}
		rings++
		b.WriteByte('(')
		for i := start; i < end; i++ {
			if i > start {
				b.WriteByte(',')
			}
			b.WriteString(strconv.FormatFloat(p.Points[i].X, 'f', -1, 64))
			b.WriteByte(' ')
			b.WriteString(strconv.FormatFloat(p.Points[i].Y, 'f', -1, 64))
		}
		b.WriteByte(')')
	}
	if rings == 0 {
		return "", errors.New("polygon has no ring that encloses an area")
	}
	b.WriteByte(')')
	return b.String(), nil
}

// BoundaryLoad is one layer's load for one state.
type BoundaryLoad struct {
	Layer      string     `json:"layer"`
	StateFIPS  string     `json:"state_fips"`
	Status     string     `json:"status"`
	Available  bool       `json:"available"`
	Features   int        `json:"features"`
	SourceURL  string     `json:"source_url"`
	Error      *string    `json:"error"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// ListBoundaryLoads reports every load, most recent first.
func ListBoundaryLoads(db *sql.DB) ([]BoundaryLoad, error) {
	rows, err := db.Query(`
		SELECT layer, state_fips, status, available, features, source_url, error, started_at, finished_at
		FROM boundary_loads
		ORDER BY started_at DESC, layer, state_fips
	`)
	if err != nil {
		if isUndefinedTable(err) {
			return []BoundaryLoad{}, nil
		}
		return nil, fmt.Errorf("failed to list boundary loads: %w", err)
	}
	defer rows.Close()
	loads := []BoundaryLoad{}
	for rows.Next() {
		var l BoundaryLoad
		if err := rows.Scan(&l.Layer, &l.StateFIPS, &l.Status, &l.Available, &l.Features,
			&l.SourceURL, &l.Error, &l.StartedAt, &l.FinishedAt); err != nil {
			return nil, fmt.Errorf("failed to read a boundary load: %w", err)
		}
		loads = append(loads, l)
	}
	return loads, rows.Err()
}
