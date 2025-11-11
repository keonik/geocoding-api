package utils

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DownloadConfig represents configuration for downloading files
type DownloadConfig struct {
	URL         string
	Destination string
	CacheDir    string
	MaxAge      time.Duration // How long to cache files
}

// FileDownloader handles downloading and caching of external files
type FileDownloader struct {
	CacheDir string
	Client   *http.Client
}

// NewFileDownloader creates a new file downloader
func NewFileDownloader(cacheDir string) *FileDownloader {
	return &FileDownloader{
		CacheDir: cacheDir,
		Client: &http.Client{
			Timeout: 30 * time.Minute, // Allow for large file downloads
		},
	}
}

// DownloadFile downloads a file with caching support
func (fd *FileDownloader) DownloadFile(config DownloadConfig) error {
	// Ensure cache directory exists
	if err := os.MkdirAll(fd.CacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Check if file exists and is not too old
	if fd.isCached(config.Destination, config.MaxAge) {
		fmt.Printf("Using cached file: %s\n", config.Destination)
		return nil
	}

	fmt.Printf("Downloading %s to %s\n", config.URL, config.Destination)

	// Create destination directory
	if err := os.MkdirAll(filepath.Dir(config.Destination), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Download the file
	resp, err := fd.Client.Get(config.URL)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Create the file
	file, err := os.Create(config.Destination)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy the response body to the file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("Successfully downloaded: %s\n", config.Destination)
	return nil
}

// ExtractZip extracts a ZIP file to a destination directory
func (fd *FileDownloader) ExtractZip(src, dest string) error {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer reader.Close()

	// Create destination directory
	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Extract files
	for _, file := range reader.File {
		path := filepath.Join(dest, file.Name)
		
		// Ensure the file path is safe
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			os.MkdirAll(path, file.FileInfo().Mode())
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// Open file in ZIP
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to open file in ZIP: %w", err)
		}

		// Create file on disk
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.FileInfo().Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("failed to create file: %w", err)
		}

		// Copy content
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()

		if err != nil {
			return fmt.Errorf("failed to copy file content: %w", err)
		}
	}

	return nil
}

// isCached checks if a file exists and is within the max age
func (fd *FileDownloader) isCached(filePath string, maxAge time.Duration) bool {
	if maxAge == 0 {
		return false // No caching if maxAge is 0
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return false // File doesn't exist
	}

	return time.Since(info.ModTime()) < maxAge
}

// GetOhioCountyList returns a list of all Ohio counties
func GetOhioCountyList() []string {
	return []string{
		"adams", "allen", "ashland", "ashtabula", "athens", "auglaize",
		"belmont", "brown", "butler", "carroll", "champaign", "clark",
		"clermont", "clinton", "columbiana", "coshocton", "crawford", "cuyahoga",
		"darke", "defiance", "delaware", "erie", "fairfield", "fayette",
		"franklin", "fulton", "gallia", "geauga", "greene", "guernsey",
		"hamilton", "hancock", "hardin", "harrison", "henry", "highland",
		"hocking", "holmes", "huron", "jackson", "jefferson", "knox",
		"lake", "lawrence", "licking", "logan", "lorain", "lucas",
		"madison", "mahoning", "marion", "medina", "meigs", "mercer",
		"miami", "monroe", "montgomery", "morgan", "morrow", "muskingum",
		"noble", "ottawa", "paulding", "perry", "pickaway", "pike",
		"portage", "preble", "putnam", "richland", "ross", "sandusky",
		"scioto", "seneca", "shelby", "stark", "summit", "trumbull",
		"tuscarawas", "union", "van_wert", "vinton", "warren", "washington",
		"wayne", "williams", "wood", "wyandot",
	}
}

// DownloadOhioData downloads Ohio address and county data using alternative sources
func (fd *FileDownloader) DownloadOhioData(destDir string) error {
	fmt.Println("Downloading Ohio county data...")

	// Try to use the real data downloader first
	realDownloader := NewRealDataDownloader(fd.CacheDir)
	if err := realDownloader.DownloadOhioRealData(destDir); err != nil {
		fmt.Printf("Real data download failed: %v\n", err)
		fmt.Println("Falling back to placeholder files...")
		
		// Fall back to creating placeholder files
		return fd.createBasicPlaceholderFiles(destDir)
	}
	
	return nil
}

// createBasicPlaceholderFiles creates basic placeholder files as a fallback
func (fd *FileDownloader) createBasicPlaceholderFiles(destDir string) error {
	// Create the destination directory
	ohDir := filepath.Join(destDir, "oh")
	if err := os.MkdirAll(ohDir, 0755); err != nil {
		return fmt.Errorf("failed to create oh directory: %w", err)
	}
	
	counties := GetOhioCountyList()
	
	for _, county := range counties {
		// Create placeholder files for now - these would be replaced with actual downloads
		addressFile := filepath.Join(ohDir, fmt.Sprintf("%s-addresses-county.geojson", county))
		metaFile := filepath.Join(ohDir, fmt.Sprintf("%s-addresses-county.geojson.meta", county))
		
		// Check if files already exist and are recent
		if fd.isCached(addressFile, 24*time.Hour) && fd.isCached(metaFile, 24*time.Hour) {
			continue
		}
		
		fmt.Printf("Creating placeholder files for %s county\n", county)
		
		// Create a minimal GeoJSON structure
		minimalGeoJSON := `{
  "type": "FeatureCollection",
  "features": []
}`
		
		if err := os.WriteFile(addressFile, []byte(minimalGeoJSON), 0644); err != nil {
			return fmt.Errorf("failed to create %s: %w", addressFile, err)
		}
		
		// Create a minimal meta file
		minimalMeta := fmt.Sprintf(`{
  "county": "%s",
  "state": "Ohio",
  "record_count": 0,
  "last_updated": "%s",
  "source": "placeholder"
}`, strings.Title(county), time.Now().Format(time.RFC3339))
		
		if err := os.WriteFile(metaFile, []byte(minimalMeta), 0644); err != nil {
			return fmt.Errorf("failed to create %s: %w", metaFile, err)
		}
	}
	
	fmt.Printf("Created placeholder files for %d Ohio counties\n", len(counties))
	return nil
}