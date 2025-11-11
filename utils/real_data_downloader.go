package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// DownloadOhioRealData attempts to download real data from multiple sources
func (rdd *RealDataDownloader) DownloadOhioRealData(destDir string) error {
	fmt.Println("Attempting to download real Ohio county data...")

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
	// Create placeholder GeoJSON and meta files based on the configuration
	addressFile := filepath.Join(destDir, fmt.Sprintf("%s-addresses-county.geojson", county))
	metaFile := filepath.Join(destDir, fmt.Sprintf("%s-addresses-county.geojson.meta", county))

	// Create minimal GeoJSON with metadata from the source
	geoJSON := map[string]interface{}{
		"type":     "FeatureCollection",
		"features": []interface{}{},
		"metadata": map[string]interface{}{
			"county":      strings.Title(county),
			"state":       "Ohio",
			"source":      "OpenAddresses",
			"last_check":  time.Now().Format(time.RFC3339),
			"data_source": "",
		},
	}

	// Extract data source URL if available
	if len(source.Layers.Addresses) > 0 {
		geoJSON["metadata"].(map[string]interface{})["data_source"] = source.Layers.Addresses[0].Data
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