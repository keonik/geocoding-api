package services

import (
	"fmt"
	"os"
	"testing"
)

// When every insert is rejected, the old code logged a warning per row, marked
// the dataset "completed" with a record count of 0, and then deleted the
// uploaded file -- destroying the only copy of the data and leaving the
// operator a green status with nothing behind it. flush() returned nil on
// every path, so the failure branch in ProcessGeoJSONDataset was unreachable.
func TestProcessGeoJSONDatasetFailsAndKeepsFileWhenEveryInsertIsRejected(t *testing.T) {
	db := ingestProbeDB(t)
	setupIngestSchema(t, db)

	// Reject every insert, standing in for a dropped connection or a
	// constraint that rejects everything.
	if _, err := db.Exec(`ALTER TABLE ohio_addresses ADD CONSTRAINT reject_all CHECK (false) NOT VALID`); err != nil {
		t.Fatalf("add rejecting constraint: %v", err)
	}

	// Enough features to fill more than maxConsecutiveFailedBatches batches.
	features := make([]probeFeature, 0, addressInsertBatchSize*maxConsecutiveFailedBatches+200)
	for i := 0; i < cap(features); i++ {
		features = append(features, pointFeature(map[string]interface{}{
			"HOUSENUM":  fmt.Sprintf("%d", 100+i),
			"ST_NAME":   "Barendt Road",
			"USPS_CITY": "Columbus",
			"ZIPCODE":   "43004",
		}, -83.0+float64(i)*0.0001, 40.0))
	}

	path := writeFixture(t, features)
	datasetID := insertProbeDataset(t, db, path)

	svc := NewDatasetService(db)
	err := svc.ProcessGeoJSONDataset(datasetID)

	if err == nil {
		t.Fatal("import reported success while every insert was rejected")
	}
	t.Logf("import correctly failed: %v", err)

	status, recordCount := datasetRow(t, db, datasetID)
	if status != "failed" {
		t.Errorf("dataset status = %q, want %q", status, "failed")
	}
	if recordCount != 0 {
		t.Errorf("record count = %d, want 0", recordCount)
	}

	// The point of the fix: the source file is the only way to retry.
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		t.Error("uploaded file was deleted after a failed import; the data is unrecoverable")
	} else if statErr != nil {
		t.Errorf("stat uploaded file: %v", statErr)
	}

	if n := countAddresses(t, db); n != 0 {
		t.Errorf("addresses imported = %d, want 0", n)
	}
}
