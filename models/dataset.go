package models

import "time"

// Dataset represents an uploaded county address dataset
type Dataset struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	State        string    `json:"state"`
	County       string    `json:"county"`
	FileType     string    `json:"file_type"` // geojson, shapefile, csv
	FilePath     string    `json:"file_path"`
	FileSize     int64     `json:"file_size"`
	RecordCount  int       `json:"record_count"`
	Status       string    `json:"status"` // pending, processing, completed, failed
	ErrorMessage string    `json:"error_message,omitempty"`
	UploadedBy   int       `json:"uploaded_by"`
	UploadedAt   time.Time `json:"uploaded_at"`
	ProcessedAt  *time.Time `json:"processed_at,omitempty"`
}

// DatasetUploadRequest represents a request to upload a dataset
type DatasetUploadRequest struct {
	Name   string `json:"name" form:"name"`
	State  string `json:"state" form:"state"`
	County string `json:"county" form:"county"`
}

// DatasetListResponse represents the response for listing datasets
type DatasetListResponse struct {
	Datasets []Dataset `json:"datasets"`
	Total    int       `json:"total"`
}

// DatasetStats represents statistics about uploaded datasets
type DatasetStats struct {
	TotalDatasets    int            `json:"total_datasets"`
	TotalRecords     int            `json:"total_records"`
	StateBreakdown   map[string]int `json:"state_breakdown"`
	StatusBreakdown  map[string]int `json:"status_breakdown"`
	TotalStorageSize int64          `json:"total_storage_size"`
}
