package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geocoding-api/models"

	_ "github.com/lib/pq"
)

// Ingestion probes for ProcessGeoJSONDataset. These cover the behaviour that
// had to survive the rewrite from "decode the whole FeatureCollection, then
// INSERT one row per feature" to "stream features, INSERT in batches":
// which features are skipped, how duplicates are counted, and that the
// PostGIS geometry and the trigger maintained full_address column still land.
//
// Skipped unless PROBE_DSN is set.

// ingestProbeSchema is a private schema these tests build their fixtures in.
//
// It exists so this file cannot disturb the other PROBE_DSN tests. Those run
// against whatever ohio_addresses happens to be in the probe database's public
// schema and assert on seeded rows; creating a table of the same name there
// made TestAddressSearchProbe find a fixture it had not built.
const ingestProbeSchema = "ingest_probe"

// ingestProbeDB returns a handle whose search_path points at a freshly created
// private schema, with public kept on the path for PostGIS.
func ingestProbeDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open probe database: %v", err)
	}
	defer admin.Close()

	if err := admin.Ping(); err != nil {
		t.Fatalf("ping probe database: %v", err)
	}
	if _, err := admin.Exec(`DROP SCHEMA IF EXISTS ` + ingestProbeSchema + ` CASCADE`); err != nil {
		t.Fatalf("drop probe schema: %v", err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + ingestProbeSchema); err != nil {
		t.Fatalf("create probe schema: %v", err)
	}

	scoped, err := sql.Open("postgres", withSearchPath(t, dsn))
	if err != nil {
		t.Fatalf("open scoped connection: %v", err)
	}
	if err := scoped.Ping(); err != nil {
		t.Fatalf("ping scoped connection: %v", err)
	}

	t.Cleanup(func() {
		scoped.Close()
		cleanup, err := sql.Open("postgres", dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		cleanup.Exec(`DROP SCHEMA IF EXISTS ` + ingestProbeSchema + ` CASCADE`)
	})

	return scoped
}

// withSearchPath adds the private schema to the DSN's search_path. lib/pq
// forwards unrecognised parameters to the server as runtime settings, so this
// applies to every connection the pool opens rather than just the first.
func withSearchPath(t *testing.T, dsn string) string {
	t.Helper()

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse PROBE_DSN: %v", err)
	}
	q := u.Query()
	q.Set("search_path", ingestProbeSchema+",public")
	u.RawQuery = q.Encode()
	return u.String()
}

// setupIngestSchema builds the slice of the schema ProcessGeoJSONDataset
// touches. The definitions mirror database/migrations.go: the ohio_addresses
// table from migration 8, the full_address column and its BEFORE INSERT
// trigger from migration 15, and the datasets table from migration 17.
// full_address matters here -- a batched multi row INSERT still has to fire
// that trigger once per row, and this is where that gets proven.
func setupIngestSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	stmts := []string{
		`CREATE TABLE ohio_addresses (
			id BIGSERIAL PRIMARY KEY,
			hash VARCHAR(255) UNIQUE NOT NULL,
			house_number VARCHAR(50),
			street VARCHAR(255),
			unit VARCHAR(50),
			city VARCHAR(255),
			district VARCHAR(10),
			region VARCHAR(2),
			postcode VARCHAR(10),
			county VARCHAR(255),
			geom GEOMETRY(POINT, 4326) NOT NULL,
			full_address TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE OR REPLACE FUNCTION update_full_address()
		RETURNS TRIGGER AS $$
		BEGIN
		  NEW.full_address := CONCAT_WS(', ',
			NULLIF(CONCAT_WS(' ',
			  NULLIF(NEW.house_number, ''),
			  NULLIF(NEW.street, ''),
			  CASE WHEN NEW.unit != '' THEN 'Unit ' || NEW.unit ELSE NULL END
			), ''),
			NULLIF(NEW.city, ''),
			CONCAT_WS(' ', NULLIF(NEW.region, ''), NULLIF(NEW.postcode, ''))
		  );
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER ohio_addresses_full_address_trigger
			BEFORE INSERT OR UPDATE ON ohio_addresses
			FOR EACH ROW EXECUTE FUNCTION update_full_address()`,
		`CREATE TABLE ingest_probe_users (id SERIAL PRIMARY KEY)`,
		`INSERT INTO ingest_probe_users (id) VALUES (1)`,
		`CREATE TABLE datasets (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			state VARCHAR(2) NOT NULL,
			county VARCHAR(255) NOT NULL,
			file_type VARCHAR(50) NOT NULL,
			file_path VARCHAR(500) NOT NULL,
			file_size BIGINT NOT NULL DEFAULT 0,
			record_count INTEGER NOT NULL DEFAULT 0,
			status VARCHAR(50) NOT NULL DEFAULT 'pending',
			error_message TEXT,
			uploaded_by INTEGER NOT NULL REFERENCES ingest_probe_users(id) ON DELETE CASCADE,
			uploaded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			processed_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema setup failed on %.60q: %v", stmt, err)
		}
	}
	// No teardown here: ingestProbeDB drops the whole private schema.
}

type probeFeature struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Geometry   map[string]interface{} `json:"geometry"`
}

func pointFeature(props map[string]interface{}, lng, lat float64) probeFeature {
	return probeFeature{
		Type:       "Feature",
		Properties: props,
		Geometry: map[string]interface{}{
			"type":        "Point",
			"coordinates": []float64{lng, lat},
		},
	}
}

// writeFixture writes a FeatureCollection to a temp file and returns its path.
func writeFixture(t *testing.T, features []probeFeature) string {
	t.Helper()

	collection := map[string]interface{}{
		"type": "FeatureCollection",
		// A member before "features" that the streaming walk has to skip past.
		"crs":      map[string]interface{}{"type": "name"},
		"features": features,
	}

	path := filepath.Join(t.TempDir(), "fixture.geojson")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(collection); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return path
}

func insertProbeDataset(t *testing.T, db *sql.DB, path string) int {
	t.Helper()

	var id int
	err := db.QueryRow(`
		INSERT INTO datasets (name, state, county, file_type, file_path, uploaded_by, status)
		VALUES ('probe', 'OH', 'Franklin', 'geojson', $1, 1, 'pending')
		RETURNING id
	`, path).Scan(&id)
	if err != nil {
		t.Fatalf("insert dataset: %v", err)
	}
	return id
}

func countAddresses(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ohio_addresses`).Scan(&n); err != nil {
		t.Fatalf("count addresses: %v", err)
	}
	return n
}

func datasetRow(t *testing.T, db *sql.DB, id int) (status string, recordCount int) {
	t.Helper()
	if err := db.QueryRow(`SELECT status, record_count FROM datasets WHERE id = $1`, id).
		Scan(&status, &recordCount); err != nil {
		t.Fatalf("read dataset row: %v", err)
	}
	return status, recordCount
}

// TestProcessGeoJSONDatasetImportSemantics is the behavioural contract: which
// features import, which are silently skipped, and which count as duplicates.
// The expected numbers are what the old row at a time implementation produced.
func TestProcessGeoJSONDatasetImportSemantics(t *testing.T) {
	db := ingestProbeDB(t)
	defer db.Close()
	setupIngestSchema(t, db)

	var features []probeFeature

	// 290 ordinary, unique addresses.
	for i := 0; i < 290; i++ {
		features = append(features, pointFeature(map[string]interface{}{
			"HOUSENUM":  fmt.Sprintf("%d", 100+i),
			"ST_NAME":   "MAIN ST",
			"USPS_CITY": "COLUMBUS",
			"ZIPCODE":   "43215",
		}, -83.0+float64(i)*0.0001, 39.96))
	}

	// A numeric house number: JSON numbers arrive as float64 and must be
	// rendered as an integer, not "1.5e+03".
	features = append(features, pointFeature(map[string]interface{}{
		"HOUSENUM":  float64(1500),
		"ST_NAME":   "NUMERIC AVE",
		"USPS_CITY": "COLUMBUS",
		"ZIPCODE":   "43215",
	}, -83.1, 39.97))

	// LSN fallback: no ST_NAME, street has to be recovered by stripping the
	// house number prefix off LSN.
	features = append(features, pointFeature(map[string]interface{}{
		"HOUSENUM":  "16551",
		"LSN":       "16551 STATE RTE 247",
		"USPS_CITY": "PEEBLES",
		"ZIPCODE":   "45660",
	}, -83.4, 38.95))

	// Lowercase OpenAddresses style keys.
	features = append(features, pointFeature(map[string]interface{}{
		"house_number": "77",
		"street":       "SUNSET BLVD",
		"city":         "DAYTON",
		"postcode":     "45402",
		"unit":         "4B",
	}, -84.19, 39.75))

	// --- features that must be skipped and NOT counted ---

	// Non-Point geometry.
	features = append(features, probeFeature{
		Type:       "Feature",
		Properties: map[string]interface{}{"HOUSENUM": "1", "ST_NAME": "LINE RD"},
		Geometry: map[string]interface{}{
			"type":        "LineString",
			"coordinates": []float64{-83.0, 39.0},
		},
	})
	// Missing street (and no LSN to recover it from).
	features = append(features, pointFeature(map[string]interface{}{
		"HOUSENUM":  "999",
		"USPS_CITY": "COLUMBUS",
	}, -83.0, 39.9))
	// Missing house number.
	features = append(features, pointFeature(map[string]interface{}{
		"ST_NAME":   "NOWHERE LN",
		"USPS_CITY": "COLUMBUS",
	}, -83.0, 39.9))

	// --- duplicates: same hash tuple as rows already above ---
	for i := 0; i < 5; i++ {
		features = append(features, pointFeature(map[string]interface{}{
			"HOUSENUM":  fmt.Sprintf("%d", 100+i),
			"ST_NAME":   "MAIN ST",
			"USPS_CITY": "COLUMBUS",
			"ZIPCODE":   "43215",
		}, -83.0, 39.96))
	}

	const wantImported = 290 + 3 // 290 plain + numeric + LSN + lowercase

	path := writeFixture(t, features)
	datasetID := insertProbeDataset(t, db, path)

	svc := NewDatasetService(db)
	if err := svc.ProcessGeoJSONDataset(datasetID); err != nil {
		t.Fatalf("ProcessGeoJSONDataset: %v", err)
	}

	if got := countAddresses(t, db); got != wantImported {
		t.Errorf("imported rows = %d, want %d", got, wantImported)
	}

	status, recordCount := datasetRow(t, db, datasetID)
	if status != "completed" {
		t.Errorf("dataset status = %q, want completed", status)
	}
	if recordCount != wantImported {
		t.Errorf("dataset record_count = %d, want %d", recordCount, wantImported)
	}

	// The LSN fallback stripped the house number prefix.
	var street string
	if err := db.QueryRow(`SELECT street FROM ohio_addresses WHERE house_number = '16551'`).Scan(&street); err != nil {
		t.Fatalf("read LSN row: %v", err)
	}
	if street != "STATE RTE 247" {
		t.Errorf("LSN street = %q, want %q", street, "STATE RTE 247")
	}

	// The numeric house number rendered as an integer.
	var numericExists bool
	if err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM ohio_addresses WHERE house_number = '1500' AND street = 'NUMERIC AVE')`,
	).Scan(&numericExists); err != nil {
		t.Fatalf("read numeric row: %v", err)
	}
	if !numericExists {
		t.Error("numeric house number was not rendered as an integer")
	}

	// county and region came from the dataset, not the feature.
	var wrongMeta int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM ohio_addresses WHERE county != 'Franklin' OR region != 'OH'`,
	).Scan(&wrongMeta); err != nil {
		t.Fatalf("check dataset metadata: %v", err)
	}
	if wrongMeta != 0 {
		t.Errorf("%d rows did not take county/region from the dataset", wrongMeta)
	}

	// The BEFORE INSERT trigger still fired for batched rows.
	var fullAddress string
	if err := db.QueryRow(
		`SELECT full_address FROM ohio_addresses WHERE house_number = '77' AND street = 'SUNSET BLVD'`,
	).Scan(&fullAddress); err != nil {
		t.Fatalf("read full_address: %v", err)
	}
	// region comes from the dataset's state, so the trailing group is "OH 45402".
	if want := "77 SUNSET BLVD Unit 4B, DAYTON, OH 45402"; fullAddress != want {
		t.Errorf("full_address = %q, want %q", fullAddress, want)
	}

	// The geometry survived batching, with lng/lat in the right order.
	var lng, lat float64
	if err := db.QueryRow(
		`SELECT ST_X(geom), ST_Y(geom) FROM ohio_addresses WHERE house_number = '77'`,
	).Scan(&lng, &lat); err != nil {
		t.Fatalf("read geom: %v", err)
	}
	if lng > -84.0 || lat < 39.0 {
		t.Errorf("geom = (%f, %f), longitude and latitude look swapped", lng, lat)
	}
	var nullGeom int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ohio_addresses WHERE geom IS NULL`).Scan(&nullGeom); err != nil {
		t.Fatalf("check null geom: %v", err)
	}
	if nullGeom != 0 {
		t.Errorf("%d rows have a NULL geom", nullGeom)
	}
}

// TestProcessGeoJSONDatasetIsIdempotent re-imports the same file. Every row
// now conflicts with one already committed, which exercises ON CONFLICT DO
// NOTHING across batches rather than the in-batch dedupe map.
func TestProcessGeoJSONDatasetIsIdempotent(t *testing.T) {
	db := ingestProbeDB(t)
	defer db.Close()
	setupIngestSchema(t, db)

	var features []probeFeature
	for i := 0; i < 50; i++ {
		features = append(features, pointFeature(map[string]interface{}{
			"HOUSENUM":  fmt.Sprintf("%d", i),
			"ST_NAME":   "REPEAT RD",
			"USPS_CITY": "COLUMBUS",
			"ZIPCODE":   "43215",
		}, -83.0, 39.96))
	}
	path := writeFixture(t, features)

	svc := NewDatasetService(db)

	first := insertProbeDataset(t, db, path)
	if err := svc.ProcessGeoJSONDataset(first); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if got := countAddresses(t, db); got != 50 {
		t.Fatalf("after first import rows = %d, want 50", got)
	}

	// ProcessGeoJSONDataset deletes the file on success, so write it again.
	path = writeFixture(t, features)
	second := insertProbeDataset(t, db, path)
	if err := svc.ProcessGeoJSONDataset(second); err != nil {
		t.Fatalf("second import: %v", err)
	}

	if got := countAddresses(t, db); got != 50 {
		t.Errorf("after re-import rows = %d, want 50 (duplicates must not insert)", got)
	}
	status, recordCount := datasetRow(t, db, second)
	if status != "completed" {
		t.Errorf("second dataset status = %q, want completed", status)
	}
	if recordCount != 0 {
		t.Errorf("second dataset record_count = %d, want 0 (all rows were duplicates)", recordCount)
	}
}

// TestProcessGeoJSONDatasetCrossesBatchBoundary uses more features than
// addressInsertBatchSize so at least three flushes happen, including a partial
// final one.
func TestProcessGeoJSONDatasetCrossesBatchBoundary(t *testing.T) {
	db := ingestProbeDB(t)
	defer db.Close()
	setupIngestSchema(t, db)

	const total = addressInsertBatchSize*2 + 137

	var features []probeFeature
	for i := 0; i < total; i++ {
		features = append(features, pointFeature(map[string]interface{}{
			"HOUSENUM":  fmt.Sprintf("%d", i),
			"ST_NAME":   fmt.Sprintf("STREET %d", i%17),
			"USPS_CITY": "COLUMBUS",
			"ZIPCODE":   "43215",
		}, -83.0, 39.96))
	}

	path := writeFixture(t, features)
	datasetID := insertProbeDataset(t, db, path)

	svc := NewDatasetService(db)
	if err := svc.ProcessGeoJSONDataset(datasetID); err != nil {
		t.Fatalf("ProcessGeoJSONDataset: %v", err)
	}

	if got := countAddresses(t, db); got != total {
		t.Errorf("imported rows = %d, want %d", got, total)
	}
	if _, recordCount := datasetRow(t, db, datasetID); recordCount != total {
		t.Errorf("record_count = %d, want %d", recordCount, total)
	}
}

// TestProcessGeoJSONDatasetMarksFailedOnTruncatedFile checks the failure path:
// a file that dies part way through must mark the dataset failed, and must keep
// the features that were parsed before the break, which is what the old per-row
// autocommit loop did.
func TestProcessGeoJSONDatasetMarksFailedOnTruncatedFile(t *testing.T) {
	db := ingestProbeDB(t)
	defer db.Close()
	setupIngestSchema(t, db)

	var features []probeFeature
	for i := 0; i < 10; i++ {
		features = append(features, pointFeature(map[string]interface{}{
			"HOUSENUM":  fmt.Sprintf("%d", i),
			"ST_NAME":   "TRUNCATED WAY",
			"USPS_CITY": "COLUMBUS",
			"ZIPCODE":   "43215",
		}, -83.0, 39.96))
	}

	full := writeFixture(t, features)
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// Chop the file mid-array.
	truncated := filepath.Join(t.TempDir(), "truncated.geojson")
	if err := os.WriteFile(truncated, raw[:len(raw)*3/4], 0o644); err != nil {
		t.Fatalf("write truncated fixture: %v", err)
	}

	datasetID := insertProbeDataset(t, db, truncated)
	svc := NewDatasetService(db)

	if err := svc.ProcessGeoJSONDataset(datasetID); err == nil {
		t.Fatal("expected an error from a truncated file, got nil")
	}

	status, _ := datasetRow(t, db, datasetID)
	if status != "failed" {
		t.Errorf("dataset status = %q, want failed", status)
	}

	var errMsg sql.NullString
	if err := db.QueryRow(`SELECT error_message FROM datasets WHERE id = $1`, datasetID).Scan(&errMsg); err != nil {
		t.Fatalf("read error_message: %v", err)
	}
	if !errMsg.Valid || errMsg.String == "" {
		t.Error("failed dataset has no error_message")
	}

	// The file must NOT have been cleaned up on the failure path.
	if _, err := os.Stat(truncated); os.IsNotExist(err) {
		t.Error("truncated file was deleted despite the import failing")
	}
}

// countingReader reports how many bytes have actually been pulled from the
// underlying stream.
type countingReader struct {
	inner io.Reader
	read  int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	r.read += n
	return n, err
}

// TestStreamGeoJSONFeaturesDoesNotBufferWholeFile is the streaming proof.
//
// The old implementation called Decode into a struct holding every feature, so
// nothing could be observed until the entire input had been consumed. Here the
// first feature has to arrive after only a small prefix of the stream has been
// read. That is a direct, deterministic observation of streaming -- no memory
// sampling, which would be flaky.
func TestStreamGeoJSONFeaturesDoesNotBufferWholeFile(t *testing.T) {
	const featureCount = 50000

	var sb strings.Builder
	sb.WriteString(`{"type":"FeatureCollection","features":[`)
	for i := 0; i < featureCount; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"type":"Feature","properties":{"HOUSENUM":"%d","ST_NAME":"LONG STREET NAME %d","USPS_CITY":"COLUMBUS","ZIPCODE":"43215"},"geometry":{"type":"Point","coordinates":[-83.0,39.96]}}`, i, i)
	}
	sb.WriteString(`]}`)

	payload := sb.String()
	if len(payload) < 5<<20 {
		t.Fatalf("fixture is only %d bytes, too small to be meaningful", len(payload))
	}

	reader := &countingReader{inner: strings.NewReader(payload)}

	var seen int
	var readAtFirstFeature int
	err := streamGeoJSONFeatures(reader, func(f geoFeature) error {
		if seen == 0 {
			readAtFirstFeature = reader.read
		}
		seen++
		return nil
	})
	if err != nil {
		t.Fatalf("streamGeoJSONFeatures: %v", err)
	}

	if seen != featureCount {
		t.Errorf("streamed %d features, want %d", seen, featureCount)
	}

	// json.Decoder reads in chunks, so allow a generous ceiling; the point is
	// that it is nowhere near the whole file.
	limit := len(payload) / 20
	if readAtFirstFeature > limit {
		t.Errorf("had read %d of %d bytes before the first feature (limit %d) -- input is being buffered, not streamed",
			readAtFirstFeature, len(payload), limit)
	}
	t.Logf("first feature delivered after reading %d of %d bytes (%.3f%%)",
		readAtFirstFeature, len(payload), 100*float64(readAtFirstFeature)/float64(len(payload)))
}

// TestAddressFromFeatureSkips pins the skip rules without needing a database.
func TestAddressFromFeatureSkips(t *testing.T) {
	tests := []struct {
		name    string
		feature geoFeature
		wantOK  bool
	}{
		{
			name:   "valid point",
			wantOK: true,
			feature: mustFeature(t, pointFeature(map[string]interface{}{
				"HOUSENUM": "1", "ST_NAME": "A ST",
			}, -83, 39)),
		},
		{
			name:   "non-point geometry",
			wantOK: false,
			feature: mustFeature(t, probeFeature{
				Type:       "Feature",
				Properties: map[string]interface{}{"HOUSENUM": "1", "ST_NAME": "A ST"},
				Geometry:   map[string]interface{}{"type": "Polygon", "coordinates": []float64{-83, 39}},
			}),
		},
		{
			name:   "missing street",
			wantOK: false,
			feature: mustFeature(t, pointFeature(map[string]interface{}{
				"HOUSENUM": "1",
			}, -83, 39)),
		},
		{
			name:   "missing house number",
			wantOK: false,
			feature: mustFeature(t, pointFeature(map[string]interface{}{
				"ST_NAME": "A ST",
			}, -83, 39)),
		},
		{
			name:   "point with truncated coordinates",
			wantOK: false,
			feature: mustFeature(t, probeFeature{
				Type:       "Feature",
				Properties: map[string]interface{}{"HOUSENUM": "1", "ST_NAME": "A ST"},
				Geometry:   map[string]interface{}{"type": "Point", "coordinates": []float64{-83}},
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := addressFromFeature(tc.feature, "Franklin", "OH")
			if ok != tc.wantOK {
				t.Errorf("addressFromFeature ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

// mustFeature round trips a probeFeature through JSON so the test exercises the
// same decoding path the streamer uses.
func mustFeature(t *testing.T, pf probeFeature) geoFeature {
	t.Helper()
	raw, err := json.Marshal(pf)
	if err != nil {
		t.Fatalf("marshal feature: %v", err)
	}
	var f geoFeature
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal feature: %v", err)
	}
	return f
}

// TestAddressHashMatchesCreateAddress guards the one piece of duplicated logic:
// the batch path builds ohio_addresses.hash itself instead of going through
// AddressService.CreateAddress, so the two formats must not drift.
func TestAddressHashMatchesCreateAddress(t *testing.T) {
	a := models.OhioAddress{
		HouseNumber: "123",
		Street:      "MAIN ST",
		Unit:        "2",
		City:        "COLUMBUS",
		Postcode:    "43215",
		County:      "Franklin",
		Region:      "OH",
	}

	// The literal below is CreateAddress's format string applied to the same
	// fields; see services/address_service.go.
	want := fmt.Sprintf("%s|%s|%s|%s|%s", a.HouseNumber, a.Street, a.Unit, a.City, a.Postcode)
	if got := addressHash(a); got != want {
		t.Errorf("addressHash = %q, want %q", got, want)
	}
	if !strings.Contains(addressHash(a), "MAIN ST") {
		t.Error("hash does not include the street")
	}
}
