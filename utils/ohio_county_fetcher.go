package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Ohio's authoritative ArcGIS services, both published by the same org.
//
// These replace the OpenAddresses + gis1.oit.ohio.gov/LBRS path the original
// downloader used, which cannot work any more: OpenAddresses carries 21 Ohio
// sources rather than one per county (there is no franklin.json), the LBRS
// host is retired, and every surviving source is an ArcGIS FeatureServer --
// exactly the case DownloadAndConvertCounty refuses.
const (
	// 88 county polygons, well inside the service's 2000-record page size.
	ohioCountyBoundariesURL = "https://services2.arcgis.com/MlJ0G8iWUyC7jAmu/ArcGIS/rest/services/countyboundaries/FeatureServer/0/query"

	// 5.5M address points; queried only for per-county counts, never downloaded.
	ohioAddressPointsURL = "https://services2.arcgis.com/MlJ0G8iWUyC7jAmu/ArcGIS/rest/services/Statewide_LBRS_Address_Points/FeatureServer/0/query"
)

type countyFeature struct {
	Properties struct {
		Name    string `json:"name"`
		FIPS    string `json:"cnty_fips"`
		Abbrev  string `json:"abbrev"`
		CapName string `json:"cap_name"`
	} `json:"properties"`
	Geometry struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
}

// countyMeta is the sidecar shape loadOhioCountyBoundaries reads. Field names
// must stay in step with the anonymous struct it unmarshals into.
type countyMeta struct {
	SourceName string                 `json:"source_name"`
	Layer      string                 `json:"layer"`
	Count      int                    `json:"count"`
	Stats      map[string]interface{} `json:"stats"`
	Bounds     struct {
		Type        string        `json:"type"`
		Coordinates [][][]float64 `json:"coordinates"`
	} `json:"bounds"`
}

func arcgisGet(endpoint string, params url.Values, out interface{}) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Get(endpoint + "?" + params.Encode())
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}

	// ArcGIS reports errors in a 200 body, so a status check is not enough.
	var probe struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &probe); err == nil && probe.Error != nil {
		return fmt.Errorf("service error: %s", probe.Error.Message)
	}

	return json.Unmarshal(body, out)
}

// fetchCountyAddressCounts returns address-point counts per county in a single
// grouped-statistics request rather than one request per county.
func fetchCountyAddressCounts() (map[string]int, error) {
	params := url.Values{}
	params.Set("where", "1=1")
	params.Set("groupByFieldsForStatistics", "COUNTY")
	params.Set("outStatistics", `[{"statisticType":"count","onStatisticField":"OBJECTID","outStatisticFieldName":"n"}]`)
	params.Set("f", "json")

	var result struct {
		Features []struct {
			Attributes struct {
				County string `json:"COUNTY"`
				N      int    `json:"n"`
			} `json:"attributes"`
		} `json:"features"`
	}
	if err := arcgisGet(ohioAddressPointsURL, params, &result); err != nil {
		return nil, fmt.Errorf("failed to fetch county address counts: %w", err)
	}

	counts := make(map[string]int, len(result.Features))
	for _, f := range result.Features {
		name := strings.TrimSpace(f.Attributes.County)
		if name == "" {
			continue // the layer carries one blank-county group
		}
		counts[strings.ToLower(name)] = f.Attributes.N
	}
	return counts, nil
}

// outerRing reduces a GeoJSON geometry to the single outer ring the loader can
// store. bounds_geometry is GEOMETRY(POLYGON, 4326), so a MultiPolygon has to
// collapse to its largest part; Lake Erie islands are dropped rather than
// failing the whole county.
func outerRing(geomType string, raw json.RawMessage) ([][]float64, error) {
	switch geomType {
	case "Polygon":
		var rings [][][]float64
		if err := json.Unmarshal(raw, &rings); err != nil {
			return nil, err
		}
		if len(rings) == 0 {
			return nil, fmt.Errorf("polygon has no rings")
		}
		return rings[0], nil

	case "MultiPolygon":
		var polys [][][][]float64
		if err := json.Unmarshal(raw, &polys); err != nil {
			return nil, err
		}
		var best [][]float64
		for _, p := range polys {
			if len(p) > 0 && len(p[0]) > len(best) {
				best = p[0]
			}
		}
		if best == nil {
			return nil, fmt.Errorf("multipolygon has no rings")
		}
		return best, nil
	}
	return nil, fmt.Errorf("unsupported geometry type %q", geomType)
}

// FetchOhioCountyBoundaries writes one oh/<county>-addresses-county.geojson.meta
// sidecar per Ohio county, which is what loadOhioCountyBoundaries reads.
//
// Boundaries come from the county polygon service rather than from the extent
// of the address points: a bounding box is not a boundary, and stray geocodes
// make it actively wrong -- Adams County's point extent spans latitude 38.58
// to 40.92, most of the western half of the state, for a county in the far
// south.
func FetchOhioCountyBoundaries(destDir string) (int, error) {
	log.Println("Fetching Ohio county boundaries from ArcGIS...")

	counts, err := fetchCountyAddressCounts()
	if err != nil {
		// Counts are supporting detail; a boundary with count 0 still renders.
		log.Printf("Warning: %v (continuing without address counts)", err)
		counts = map[string]int{}
	}

	params := url.Values{}
	params.Set("where", "1=1")
	params.Set("outFields", "name,cnty_fips,abbrev,cap_name")
	params.Set("outSR", "4326")
	params.Set("f", "geojson")

	var fc struct {
		Features []countyFeature `json:"features"`
	}
	if err := arcgisGet(ohioCountyBoundariesURL, params, &fc); err != nil {
		return 0, fmt.Errorf("failed to fetch county boundaries: %w", err)
	}
	if len(fc.Features) == 0 {
		return 0, fmt.Errorf("county boundary service returned no features")
	}

	ohDir := filepath.Join(destDir, "oh")
	if err := os.MkdirAll(ohDir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create %s: %w", ohDir, err)
	}

	written := 0
	for _, f := range fc.Features {
		name := strings.TrimSpace(f.Properties.Name)
		if name == "" {
			continue
		}

		ring, err := outerRing(f.Geometry.Type, f.Geometry.Coordinates)
		if err != nil {
			log.Printf("Warning: skipping %s: %v", name, err)
			continue
		}
		if len(ring) < 4 {
			log.Printf("Warning: skipping %s: ring has only %d points", name, len(ring))
			continue
		}

		meta := countyMeta{
			SourceName: fmt.Sprintf("%s-addresses-county", strings.ToLower(name)),
			Layer:      "addresses",
			Count:      counts[strings.ToLower(name)],
			Stats: map[string]interface{}{
				"source":     "Ohio ArcGIS countyboundaries + Statewide_LBRS_Address_Points",
				"fips":       f.Properties.FIPS,
				"abbrev":     f.Properties.Abbrev,
				"fetched_at": time.Now().UTC().Format(time.RFC3339),
			},
		}
		meta.Bounds.Type = "Polygon"
		meta.Bounds.Coordinates = [][][]float64{ring}

		blob, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			log.Printf("Warning: failed to encode %s: %v", name, err)
			continue
		}

		// The loader turns the filename back into a county name by replacing
		// underscores with spaces, so "Van Wert" must be written van_wert.
		slug := strings.ReplaceAll(strings.ToLower(name), " ", "_")
		path := filepath.Join(ohDir, slug+"-addresses-county.geojson.meta")
		if err := os.WriteFile(path, blob, 0644); err != nil {
			log.Printf("Warning: failed to write %s: %v", path, err)
			continue
		}
		written++
	}

	log.Printf("Wrote %d Ohio county boundary meta files to %s", written, ohDir)
	return written, nil
}
