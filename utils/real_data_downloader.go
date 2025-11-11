package utils

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// OpenAddressesSource represents a source configuration from OpenAddresses
type OpenAddressesSource struct {
	Coverage struct {
		Country string `json:"country"`
		State   string `json:"state"`
		County  string `json:"county"`
	} `json:"coverage"`
	Layers struct {
		Addresses []struct {
			Name     string `json:"name"`
			Data     string `json:"data"`
			Protocol string `json:"protocol"`
		} `json:"addresses"`
	} `json:"layers"`
}

// RealDataDownloader handles downloading real data from various sources
type RealDataDownloader struct {
	Client   *http.Client
	CacheDir string
}

// NewRealDataDownloader creates a new real data downloader
func NewRealDataDownloader(cacheDir string) *RealDataDownloader {
	return &RealDataDownloader{
		Client: &http.Client{
			Timeout: 30 * time.Minute,
		},
		CacheDir: cacheDir,
	}
}

// CheckGDALInstallation checks if GDAL/ogr2ogr is installed
func (rdd *RealDataDownloader) CheckGDALInstallation() error {
	if _, err := exec.LookPath("ogr2ogr"); err != nil {
		return fmt.Errorf(`ogr2ogr not found. Please install GDAL:
		
macOS:    brew install gdal
Ubuntu:   sudo apt-get install gdal-bin
Windows:  Download from https://gdal.org/download.html

After installation, verify with: ogr2ogr --version`)
	}

	// Check version
	cmd := exec.Command("ogr2ogr", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check ogr2ogr version: %w", err)
	}

	fmt.Printf("GDAL found: %s\n", string(output))
	return nil
}

// DownloadOhioRealData attempts to download real data from multiple sources
func (rdd *RealDataDownloader) DownloadOhioRealData(destDir string) error {
	fmt.Println("Attempting to download real Ohio county data...")

	// Check if GDAL is installed
	if err := rdd.CheckGDALInstallation(); err != nil {
		fmt.Printf("Warning: %v\n", err)
		fmt.Println("Continuing without shapefile conversion capability...")
	}

	// Create destination directory
	ohDir := filepath.Join(destDir, "oh")
	if err := os.MkdirAll(ohDir, 0755); err != nil {
		return fmt.Errorf("failed to create oh directory: %w", err)
	}

	// Try multiple data sources
	sources := []DataSource{
		{
			Name:        "Ohio Statewide Addressing System",
			Description: "Official Ohio address data",
			URLs: map[string]string{
				"statewide": "https://gis.dot.state.oh.us/tims/Data/Download",
			},
			Type: "reference",
		},
		{
			Name:        "US Census Bureau",
			Description: "Census address data for Ohio",
			URLs: map[string]string{
				"ohio": "https://www2.census.gov/geo/tiger/TIGER2023/ADDR/",
			},
			Type: "census",
		},
		{
			Name:        "OpenAddresses GitHub Raw",
			Description: "Raw OpenAddresses configuration files",
			URLs:        rdd.generateOpenAddressesURLs(),
			Type:        "openaddresses",
		},
	}

	successCount := 0
	for _, source := range sources {
		fmt.Printf("Trying source: %s\n", source.Name)
		
		switch source.Type {
		case "openaddresses":
			if err := rdd.downloadOpenAddressesConfigs(source.URLs, ohDir); err != nil {
				fmt.Printf("Warning: Failed to download from %s: %v\n", source.Name, err)
			} else {
				successCount++
			}
		case "reference":
			fmt.Printf("Reference source: %s - %s\n", source.Name, source.Description)
		case "census":
			fmt.Printf("Census source: %s - %s\n", source.Name, source.Description)
		}
	}

	if successCount == 0 {
		// Fall back to creating placeholder files
		fmt.Println("No real data sources available, creating placeholder files...")
		return rdd.createPlaceholderFiles(ohDir)
	}

	fmt.Printf("Successfully downloaded data from %d sources\n", successCount)
	return nil
}

// DataSource represents a data source configuration
type DataSource struct {
	Name        string
	Description string
	URLs        map[string]string
	Type        string
}

// generateOpenAddressesURLs generates URLs for OpenAddresses configuration files
func (rdd *RealDataDownloader) generateOpenAddressesURLs() map[string]string {
	baseURL := "https://raw.githubusercontent.com/openaddresses/openaddresses/master/sources/us/oh"
	counties := GetOhioCountyList()
	urls := make(map[string]string)
	
	for _, county := range counties {
		urls[county] = fmt.Sprintf("%s/%s.json", baseURL, county)
	}
	
	return urls
}

// downloadOpenAddressesConfigs downloads OpenAddresses configuration files
func (rdd *RealDataDownloader) downloadOpenAddressesConfigs(urls map[string]string, destDir string) error {
	successCount := 0
	
	for county, url := range urls {
		// Download the configuration file
		resp, err := rdd.Client.Get(url)
		if err != nil {
			fmt.Printf("Warning: Failed to download %s config: %v\n", county, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Warning: %s config returned status %d\n", county, resp.StatusCode)
			continue
		}

		// Parse the configuration
		var source OpenAddressesSource
		if err := json.NewDecoder(resp.Body).Decode(&source); err != nil {
			fmt.Printf("Warning: Failed to parse %s config: %v\n", county, err)
			continue
		}

		// Create files based on the configuration
		if err := rdd.processOpenAddressesConfig(county, &source, destDir); err != nil {
			fmt.Printf("Warning: Failed to process %s config: %v\n", county, err)
			continue
		}

		successCount++
	}

	if successCount == 0 {
		return fmt.Errorf("failed to download any OpenAddresses configurations")
	}

	fmt.Printf("Successfully processed %d OpenAddresses configurations\n", successCount)
	return nil
}

// processOpenAddressesConfig processes an OpenAddresses configuration
func (rdd *RealDataDownloader) processOpenAddressesConfig(county string, source *OpenAddressesSource, destDir string) error {
	addressFile := filepath.Join(destDir, fmt.Sprintf("%s-addresses-county.geojson", county))
	metaFile := filepath.Join(destDir, fmt.Sprintf("%s-addresses-county.geojson.meta", county))

	// Extract data source URL if available
	dataSourceURL := ""
	if len(source.Layers.Addresses) > 0 {
		dataSourceURL = source.Layers.Addresses[0].Data
	}

	// If we have a real data source URL (Ohio LBRS), download it
	if strings.Contains(dataSourceURL, "gis1.oit.ohio.gov/LBRS") {
		fmt.Printf("Downloading real data from Ohio LBRS for %s...\n", county)
		
		// Download the ZIP file
		zipPath := filepath.Join(rdd.CacheDir, fmt.Sprintf("%s_ADDS.zip", strings.ToUpper(county[:3])))
		
		// Check if already downloaded and recent
		if !rdd.isCached(zipPath, 24*time.Hour) {
			if err := rdd.DownloadFileFromURL(dataSourceURL, zipPath); err != nil {
				fmt.Printf("Warning: Failed to download %s data: %v\n", county, err)
				return rdd.createPlaceholderFile(county, dataSourceURL, destDir)
			}
		} else {
			fmt.Printf("Using cached ZIP file for %s\n", county)
		}

		// Check if GeoJSON already exists and is recent
		if rdd.isCached(addressFile, 24*time.Hour) {
			fmt.Printf("Using cached GeoJSON file for %s\n", county)
		} else {
			// Extract and convert to GeoJSON
			if err := rdd.convertShapefileToGeoJSON(zipPath, addressFile, county); err != nil {
				fmt.Printf("Warning: Failed to convert shapefile for %s: %v\n", county, err)
				return rdd.createPlaceholderFile(county, dataSourceURL, destDir)
			}
		}
	} else {
		// No real data source, create placeholder
		return rdd.createPlaceholderFile(county, dataSourceURL, destDir)
	}

	// Create meta file
	meta := map[string]interface{}{
		"county":       strings.Title(county),
		"state":        "Ohio",
		"last_updated": time.Now().Format(time.RFC3339),
		"source":       "OpenAddresses",
		"data_source":  dataSourceURL,
		"coverage":     source.Coverage,
		"layers":       source.Layers,
	}

	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal meta data: %w", err)
	}

	if err := os.WriteFile(metaFile, metaData, 0644); err != nil {
		return fmt.Errorf("failed to write meta file: %w", err)
	}

	return nil
}

// createPlaceholderFile creates a single placeholder file for a county
func (rdd *RealDataDownloader) createPlaceholderFile(county, dataSourceURL, destDir string) error {
	addressFile := filepath.Join(destDir, fmt.Sprintf("%s-addresses-county.geojson", county))
	metaFile := filepath.Join(destDir, fmt.Sprintf("%s-addresses-county.geojson.meta", county))

	// Create minimal GeoJSON with metadata
	geoJSON := map[string]interface{}{
		"type":     "FeatureCollection",
		"features": []interface{}{},
		"metadata": map[string]interface{}{
			"county":      strings.Title(county),
			"state":       "Ohio",
			"source":      "OpenAddresses",
			"last_check":  time.Now().Format(time.RFC3339),
			"data_source": dataSourceURL,
		},
	}

	// Write GeoJSON file
	geoJSONData, err := json.MarshalIndent(geoJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal GeoJSON: %w", err)
	}

	if err := os.WriteFile(addressFile, geoJSONData, 0644); err != nil {
		return fmt.Errorf("failed to write GeoJSON file: %w", err)
	}

	// Create meta file
	meta := map[string]interface{}{
		"county":       strings.Title(county),
		"state":        "Ohio",
		"record_count": 0,
		"last_updated": time.Now().Format(time.RFC3339),
		"source":       "OpenAddresses",
		"data_source":  dataSourceURL,
	}

	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal meta data: %w", err)
	}

	if err := os.WriteFile(metaFile, metaData, 0644); err != nil {
		return fmt.Errorf("failed to write meta file: %w", err)
	}

	return nil
}

// convertShapefileToGeoJSON converts a shapefile ZIP to GeoJSON using ogr2ogr
func (rdd *RealDataDownloader) convertShapefileToGeoJSON(zipPath, outputPath, county string) error {
	// Check if ogr2ogr is available
	if _, err := exec.LookPath("ogr2ogr"); err != nil {
		return fmt.Errorf("ogr2ogr not found. Please install GDAL: %w", err)
	}

	// Create a temporary directory for extraction
	tempDir := filepath.Join(rdd.CacheDir, fmt.Sprintf("temp_%s", county))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Extract the ZIP file
	if err := rdd.extractZip(zipPath, tempDir); err != nil {
		return fmt.Errorf("failed to extract ZIP: %w", err)
	}

	// Find the .shp file in the extracted contents
	shpFile, err := rdd.findShapefile(tempDir)
	if err != nil {
		return fmt.Errorf("failed to find shapefile: %w", err)
	}

	fmt.Printf("Converting shapefile %s to GeoJSON...\n", shpFile)

	// Convert shapefile to GeoJSON using ogr2ogr
	cmd := exec.Command("ogr2ogr",
		"-f", "GeoJSON",
		"-t_srs", "EPSG:4326", // Ensure WGS84 coordinate system
		outputPath,
		shpFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ogr2ogr failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("Successfully converted %s to GeoJSON\n", county)
	return nil
}

// extractZip extracts a ZIP file to a destination directory
func (rdd *RealDataDownloader) extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open ZIP: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(destDir, f.Name)

		// Check for ZipSlip vulnerability
		if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		// Extract file
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

// findShapefile finds a .shp file in a directory
func (rdd *RealDataDownloader) findShapefile(dir string) (string, error) {
	var shpFile string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".shp") {
			shpFile = path
			return filepath.SkipDir // Stop after finding first .shp file
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	if shpFile == "" {
		return "", fmt.Errorf("no .shp file found in directory")
	}

	return shpFile, nil
}

// createPlaceholderFiles creates placeholder files when no real data is available
func (rdd *RealDataDownloader) createPlaceholderFiles(destDir string) error {
	counties := GetOhioCountyList()
	
	for _, county := range counties {
		addressFile := filepath.Join(destDir, fmt.Sprintf("%s-addresses-county.geojson", county))
		metaFile := filepath.Join(destDir, fmt.Sprintf("%s-addresses-county.geojson.meta", county))
		
		// Skip if files already exist and are recent
		if rdd.isCached(addressFile, 24*time.Hour) && rdd.isCached(metaFile, 24*time.Hour) {
			continue
		}
		
		// Create minimal GeoJSON structure
		geoJSON := map[string]interface{}{
			"type":     "FeatureCollection",
			"features": []interface{}{},
			"metadata": map[string]interface{}{
				"county":      strings.Title(county),
				"state":       "Ohio",
				"record_count": 0,
				"last_updated": time.Now().Format(time.RFC3339),
				"source":      "placeholder",
				"note":        "This is a placeholder file. Real data needs to be downloaded from appropriate sources.",
			},
		}
		
		geoJSONData, _ := json.MarshalIndent(geoJSON, "", "  ")
		if err := os.WriteFile(addressFile, geoJSONData, 0644); err != nil {
			return fmt.Errorf("failed to create %s: %w", addressFile, err)
		}
		
		// Create meta file
		meta := map[string]interface{}{
			"county":       strings.Title(county),
			"state":        "Ohio",
			"record_count": 0,
			"last_updated": time.Now().Format(time.RFC3339),
			"source":       "placeholder",
			"note":         "This is a placeholder file. Real data needs to be downloaded from appropriate sources.",
		}
		
		metaData, _ := json.MarshalIndent(meta, "", "  ")
		if err := os.WriteFile(metaFile, metaData, 0644); err != nil {
			return fmt.Errorf("failed to create %s: %w", metaFile, err)
		}
	}
	
	fmt.Printf("Created placeholder files for %d Ohio counties\n", len(counties))
	return nil
}

// isCached checks if a file exists and is within the max age
func (rdd *RealDataDownloader) isCached(filePath string, maxAge time.Duration) bool {
	if maxAge == 0 {
		return false
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}

	return time.Since(info.ModTime()) < maxAge
}

// DownloadFileFromURL downloads a file from a URL with progress tracking
func (rdd *RealDataDownloader) DownloadFileFromURL(url, destination string) error {
	fmt.Printf("Downloading %s...\n", url)
	
	resp, err := rdd.Client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Create destination directory
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create the file
	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy with progress tracking
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("Successfully downloaded: %s\n", destination)
	return nil
}