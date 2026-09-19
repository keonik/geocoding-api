package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
}

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

// tigerURL is where one state's file for a layer lives.
func tigerURL(layer BoundaryLayer, stateFIPS string) string {
	return fmt.Sprintf("%s/%s/tl_2025_%s_%s.zip", tigerBaseURL, layer.Dir, stateFIPS, layer.Suffix)
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

// BeginBoundaryLoad claims a layer and state for loading, so two requests
// cannot load the same file into the same rows at once. The caller then runs
// LoadBoundaryLayer, which finishes the claim either way.
func BeginBoundaryLoad(db *sql.DB, layer BoundaryLayer, stateFIPS string) error {
	if err := database.RequireSchemaVersion(db, database.SchemaVersionBoundaries); err != nil {
		return err
	}
	if !stateFIPSPattern.MatchString(stateFIPS) {
		return fmt.Errorf("state FIPS %q is not two digits", stateFIPS)
	}
	res, err := db.Exec(`
		INSERT INTO boundary_loads (layer, state_fips, status, source_url, started_at)
		VALUES ($1, $2, 'loading', $3, NOW())
		ON CONFLICT (layer, state_fips) DO UPDATE
		SET status = 'loading', source_url = EXCLUDED.source_url, error = NULL,
		    started_at = NOW(), finished_at = NULL
		WHERE boundary_loads.status <> 'loading'
		   OR boundary_loads.started_at < NOW() - make_interval(secs => $4)
	`, layer.Name, stateFIPS, tigerURL(layer, stateFIPS), staleLoadAfter.Seconds())
	if err != nil {
		return fmt.Errorf("failed to record the boundary load: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrBoundaryLoadInProgress
	}
	return nil
}

// LoadBoundaryLayer downloads one state's TIGER file for a layer and replaces
// that state's rows with it.
//
// The replace is one transaction: a lookup during the load sees the old
// polygons or the new ones, never a state half-loaded, and a load that fails
// leaves the previous one serving. The outcome is written
// to boundary_loads whatever happens, since a load is started from a request
// that has long since returned.
func LoadBoundaryLayer(ctx context.Context, db *sql.DB, layer BoundaryLayer, stateFIPS string) (int, error) {
	n, status, err := loadBoundaryLayer(ctx, db, layer, stateFIPS)

	errText := sql.NullString{}
	if err != nil {
		status = "failed"
		errText = sql.NullString{String: err.Error(), Valid: true}
	}
	if _, uerr := db.Exec(`
		UPDATE boundary_loads
		SET status = $3::text, error = $5, finished_at = NOW(),
		    -- A failure rolled back, so whatever was serving still is.
		    available = CASE WHEN $3::text IN ('loaded', 'absent') THEN true ELSE available END,
		    features = CASE WHEN $3::text IN ('loaded', 'absent') THEN $4 ELSE features END
		WHERE layer = $1 AND state_fips = $2
	`, layer.Name, stateFIPS, status, n, errText); uerr != nil && err == nil {
		err = fmt.Errorf("loaded, but failed to record it: %w", uerr)
	}
	return n, err
}

func loadBoundaryLayer(ctx context.Context, db *sql.DB, layer BoundaryLayer, stateFIPS string) (int, string, error) {
	path, found, err := downloadTigerFile(ctx, tigerURL(layer, stateFIPS))
	if err != nil {
		return 0, "", err
	}
	if !found {
		// Not an error: the state has no such layer. Any rows from an
		// earlier load are stale by the same token.
		if _, err := db.Exec(`DELETE FROM boundaries WHERE layer = $1 AND state_fips = $2`, layer.Name, stateFIPS); err != nil {
			return 0, "", fmt.Errorf("failed to clear %s for state %s: %w", layer.Name, stateFIPS, err)
		}
		return 0, "absent", nil
	}
	defer os.Remove(path)

	n, err := replaceBoundaries(ctx, db, layer, stateFIPS, path)
	if err != nil {
		return 0, "", err
	}
	return n, "loaded", nil
}

// downloadTigerFile fetches a zip to a temporary file. found is false when the
// Census has no such file, which for TIGER means the layer does not exist for
// that state.
func downloadTigerFile(ctx context.Context, url string) (path string, found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", "geocoding-api boundary loader")

	resp, err := http.DefaultClient.Do(req)
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

// boundaryInsertBatch is how many features go in one INSERT. Block groups
// average ~2 KB of WKT each; 200 keeps a statement to a few hundred KB.
const boundaryInsertBatch = 200

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
	geoidCol, ok := col("GEOID")
	if !ok {
		return 0, fmt.Errorf("%s shapefile has no GEOID column", layer.Name)
	}
	nameCol, ok := col("NAMELSAD")
	if !ok {
		if nameCol, ok = col("NAME"); !ok {
			return 0, fmt.Errorf("%s shapefile has no NAME or NAMELSAD column", layer.Name)
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

	if _, err := tx.Exec(`DELETE FROM boundaries WHERE layer = $1 AND state_fips = $2`, layer.Name, stateFIPS); err != nil {
		return 0, fmt.Errorf("failed to clear %s for state %s: %w", layer.Name, stateFIPS, err)
	}

	var batch []interface{}
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
		if _, err := tx.Exec(`INSERT INTO boundaries (layer, geoid, state_fips, name, attrs, geom) VALUES `+
			strings.Join(values, ", "), args...); err != nil {
			// A feature whose rings form no area fails NOT NULL on geom; the
			// range is what finds it in a file of thousands.
			return fmt.Errorf("failed to insert %s boundaries %s..%s: %w", layer.Name, firstGEOID, lastGEOID, err)
		}
		batch = batch[:0]
		return nil
	}

	for z.Next() {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		_, shape := z.Shape()
		poly, ok := shape.(*shp.Polygon)
		if !ok {
			return 0, fmt.Errorf("%s shapefile holds %T, not polygons", layer.Name, shape)
		}
		attrs := map[string]string{}
		for dbf, key := range layer.Attrs {
			if i, ok := cols[dbf]; ok {
				attrs[key] = attr(i)
			}
		}
		attrJSON, err := json.Marshal(attrs)
		if err != nil {
			return 0, err
		}
		geoid := attr(geoidCol)
		if len(batch) == 0 {
			firstGEOID = geoid
		}
		lastGEOID = geoid
		batch = append(batch, geoid, attr(nameCol), string(attrJSON), ringsWKT(poly))
		n++
		if n%boundaryInsertBatch == 0 {
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
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit %s boundaries: %w", layer.Name, err)
	}
	return n, nil
}

// ringsWKT writes a shapefile polygon's rings as a MULTILINESTRING, for
// ST_BuildArea to assemble.
func ringsWKT(p *shp.Polygon) string {
	var b strings.Builder
	b.WriteString("MULTILINESTRING(")
	for part := 0; part < int(p.NumParts); part++ {
		start := int(p.Parts[part])
		end := len(p.Points)
		if part+1 < int(p.NumParts) {
			end = int(p.Parts[part+1])
		}
		if part > 0 {
			b.WriteByte(',')
		}
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
	b.WriteByte(')')
	return b.String()
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
