package database

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// RunMigrations runs all database migrations in order
func RunMigrations() error {
	log.Println("Running database migrations...")

	migrations := []Migration{
		{
			Version:     1,
			Description: "Create zip_codes table",
			Up:          createZipCodesTable,
			Down:        dropZipCodesTable,
		},
		{
			Version:     2,
			Description: "Create migration tracking table",
			Up:          createMigrationsTable,
			Down:        dropMigrationsTable,
		},
		{
			Version:     3,
			Description: "Create authentication tables",
			Up:          createAuthTables,
			Down:        dropAuthTables,
		},
		{
			Version:     4,
			Description: "Add name and company fields to users table",
			Up:          addUserFields,
			Down:        removeUserFields,
		},
		{
			Version:     5,
			Description: "Add key_preview and expires_at to api_keys table",
			Up:          addAPIKeyFields,
			Down:        removeAPIKeyFields,
		},
		{
			Version:     6,
			Description: "Update subscriptions table with billing columns",
			Up:          updateSubscriptionsTable,
			Down:        revertSubscriptionsTable,
		},
		{
			Version:     7,
			Description: "Add admin role to users table",
			Up:          addAdminRole,
			Down:        removeAdminRole,
		},
		{
			Version:     8,
			Description: "Create Ohio addresses table and load county data",
			Up:          createOhioAddressesTable,
			Down:        dropOhioAddressesTable,
		},
		{
			Version:     9,
			Description: "Create Ohio counties table and load boundary data",
			Up:          createOhioCountiesTable,
			Down:        dropOhioCountiesTable,
		},
	}

	// Create migrations table if it doesn't exist
	if err := createMigrationsTable(); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Run pending migrations
	for _, migration := range migrations {
		applied, err := isMigrationApplied(migration.Version)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}

		if !applied {
			log.Printf("Running migration %d: %s", migration.Version, migration.Description)
			if err := migration.Up(); err != nil {
				return fmt.Errorf("failed to run migration %d: %w", migration.Version, err)
			}

			if err := markMigrationApplied(migration.Version, migration.Description); err != nil {
				return fmt.Errorf("failed to mark migration as applied: %w", err)
			}
		} else {
			log.Printf("Migration %d already applied: %s", migration.Version, migration.Description)
		}
	}

	log.Println("All migrations completed successfully")
	return nil
}

// Migration represents a database migration
type Migration struct {
	Version     int
	Description string
	Up          func() error
	Down        func() error
}

// createMigrationsTable creates the schema_migrations table
func createMigrationsTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		description TEXT NOT NULL
	)`
	
	_, err := DB.Exec(query)
	return err
}

// dropMigrationsTable drops the schema_migrations table
func dropMigrationsTable() error {
	_, err := DB.Exec("DROP TABLE IF EXISTS schema_migrations")
	return err
}

// createZipCodesTable creates the zip_codes table
func createZipCodesTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS zip_codes (
		zip_code VARCHAR(10) PRIMARY KEY,
		city_name VARCHAR(255) NOT NULL,
		state_code VARCHAR(2) NOT NULL,
		state_name VARCHAR(255) NOT NULL,
		zcta BOOLEAN NOT NULL DEFAULT FALSE,
		zcta_parent VARCHAR(10),
		population DECIMAL(12,2),
		density DECIMAL(10,2),
		primary_county_code VARCHAR(10) NOT NULL,
		primary_county_name VARCHAR(255) NOT NULL,
		county_weights JSONB,
		county_names TEXT,
		county_codes TEXT,
		imprecise BOOLEAN NOT NULL DEFAULT FALSE,
		military BOOLEAN NOT NULL DEFAULT FALSE,
		timezone VARCHAR(100) NOT NULL,
		latitude DECIMAL(10,7) NOT NULL,
		longitude DECIMAL(10,7) NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Create indexes for better query performance
	CREATE INDEX IF NOT EXISTS idx_zip_codes_state_code ON zip_codes(state_code);
	CREATE INDEX IF NOT EXISTS idx_zip_codes_city_name ON zip_codes(city_name);
	CREATE INDEX IF NOT EXISTS idx_zip_codes_state_name ON zip_codes(state_name);
	CREATE INDEX IF NOT EXISTS idx_zip_codes_county_name ON zip_codes(primary_county_name);
	CREATE INDEX IF NOT EXISTS idx_zip_codes_location ON zip_codes(latitude, longitude);
	`
	
	_, err := DB.Exec(query)
	return err
}

// dropZipCodesTable drops the zip_codes table
func dropZipCodesTable() error {
	_, err := DB.Exec("DROP TABLE IF EXISTS zip_codes")
	return err
}

// isMigrationApplied checks if a migration has been applied
func isMigrationApplied(version int) (bool, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", version).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// markMigrationApplied marks a migration as applied
func markMigrationApplied(version int, description string) error {
	_, err := DB.Exec("INSERT INTO schema_migrations (version, description) VALUES ($1, $2)", version, description)
	return err
}

// createAuthTables creates all authentication related tables
func createAuthTables() error {
	query := `
	-- Users table
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		email VARCHAR(255) UNIQUE NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		plan_type VARCHAR(50) DEFAULT 'free' CHECK (plan_type IN ('free', 'basic', 'pro', 'enterprise')),
		is_active BOOLEAN DEFAULT true,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- API Keys table
	CREATE TABLE IF NOT EXISTS api_keys (
		id SERIAL PRIMARY KEY,
		user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
		name VARCHAR(255) NOT NULL,
		key_hash VARCHAR(255) NOT NULL UNIQUE,
		permissions TEXT[], -- Array of permission strings
		is_active BOOLEAN DEFAULT true,
		last_used_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Usage Records table
	CREATE TABLE IF NOT EXISTS usage_records (
		id SERIAL PRIMARY KEY,
		user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
		api_key_id INTEGER REFERENCES api_keys(id) ON DELETE CASCADE,
		endpoint VARCHAR(100) NOT NULL,
		method VARCHAR(10) NOT NULL,
		status_code INTEGER,
		response_time_ms INTEGER,
		ip_address INET,
		user_agent TEXT,
		billable BOOLEAN DEFAULT true,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Subscriptions table (for tracking billing periods and usage limits)
	CREATE TABLE IF NOT EXISTS subscriptions (
		id SERIAL PRIMARY KEY,
		user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
		plan_type VARCHAR(50) NOT NULL,
		monthly_limit INTEGER NOT NULL,
		current_usage INTEGER DEFAULT 0,
		billing_period_start DATE NOT NULL,
		billing_period_end DATE NOT NULL,
		is_active BOOLEAN DEFAULT true,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Create indexes for performance
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE INDEX IF NOT EXISTS idx_users_plan_type ON users(plan_type);
	CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
	CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
	CREATE INDEX IF NOT EXISTS idx_usage_records_user_id ON usage_records(user_id);
	CREATE INDEX IF NOT EXISTS idx_usage_records_api_key_id ON usage_records(api_key_id);
	CREATE INDEX IF NOT EXISTS idx_usage_records_created_at ON usage_records(created_at);
	CREATE INDEX IF NOT EXISTS idx_usage_records_endpoint ON usage_records(endpoint);
	CREATE INDEX IF NOT EXISTS idx_usage_records_billable ON usage_records(billable);
	CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);
	CREATE INDEX IF NOT EXISTS idx_subscriptions_billing_period ON subscriptions(billing_period_start, billing_period_end);

	-- Create a function to update the updated_at timestamp
	CREATE OR REPLACE FUNCTION update_updated_at_column()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP;
		RETURN NEW;
	END;
	$$ language 'plpgsql';

	-- Create triggers to automatically update the updated_at column
	DROP TRIGGER IF EXISTS update_users_updated_at ON users;
	CREATE TRIGGER update_users_updated_at 
		BEFORE UPDATE ON users 
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

	DROP TRIGGER IF EXISTS update_api_keys_updated_at ON api_keys;
	CREATE TRIGGER update_api_keys_updated_at 
		BEFORE UPDATE ON api_keys 
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

	DROP TRIGGER IF EXISTS update_subscriptions_updated_at ON subscriptions;
	CREATE TRIGGER update_subscriptions_updated_at 
		BEFORE UPDATE ON subscriptions 
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
	`
	
	_, err := DB.Exec(query)
	return err
}

// dropAuthTables drops all authentication related tables
func dropAuthTables() error {
	query := `
	DROP TRIGGER IF EXISTS update_subscriptions_updated_at ON subscriptions;
	DROP TRIGGER IF EXISTS update_api_keys_updated_at ON api_keys;
	DROP TRIGGER IF EXISTS update_users_updated_at ON users;
	DROP FUNCTION IF EXISTS update_updated_at_column();
	DROP TABLE IF EXISTS subscriptions;
	DROP TABLE IF EXISTS usage_records;
	DROP TABLE IF EXISTS api_keys;
	DROP TABLE IF EXISTS users;
	`
	
	_, err := DB.Exec(query)
	return err
}

// addUserFields adds name and company fields to users table
func addUserFields() error {
	query := `
	ALTER TABLE users 
	ADD COLUMN IF NOT EXISTS name VARCHAR(255),
	ADD COLUMN IF NOT EXISTS company VARCHAR(255);
	`
	
	_, err := DB.Exec(query)
	return err
}

// removeUserFields removes name and company fields from users table
func removeUserFields() error {
	query := `
	ALTER TABLE users 
	DROP COLUMN IF EXISTS name,
	DROP COLUMN IF EXISTS company;
	`
	
	_, err := DB.Exec(query)
	return err
}

// addAPIKeyFields adds key_preview and expires_at fields to api_keys table
func addAPIKeyFields() error {
	query := `
	ALTER TABLE api_keys 
	ADD COLUMN IF NOT EXISTS key_preview VARCHAR(50),
	ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;
	`
	
	_, err := DB.Exec(query)
	return err
}

// removeAPIKeyFields removes key_preview and expires_at fields from api_keys table
func removeAPIKeyFields() error {
	query := `
	ALTER TABLE api_keys 
	DROP COLUMN IF EXISTS key_preview,
	DROP COLUMN IF EXISTS expires_at;
	`
	
	_, err := DB.Exec(query)
	return err
}

// updateSubscriptionsTable adds missing columns to subscriptions table for proper billing
func updateSubscriptionsTable() error {
	query := `
	-- Add missing columns to subscriptions table
	ALTER TABLE subscriptions 
	ADD COLUMN IF NOT EXISTS status VARCHAR(50) DEFAULT 'active' CHECK (status IN ('active', 'cancelled', 'past_due', 'trialing')),
	ADD COLUMN IF NOT EXISTS current_period_start TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	ADD COLUMN IF NOT EXISTS current_period_end TIMESTAMP DEFAULT (CURRENT_TIMESTAMP + INTERVAL '1 month'),
	ADD COLUMN IF NOT EXISTS price_per_call DECIMAL(10,6) DEFAULT 0.0,
	ADD COLUMN IF NOT EXISTS stripe_customer_id VARCHAR(255),
	ADD COLUMN IF NOT EXISTS stripe_subscription_id VARCHAR(255);

	-- Remove old columns that are no longer used
	ALTER TABLE subscriptions 
	DROP COLUMN IF EXISTS billing_period_start,
	DROP COLUMN IF EXISTS billing_period_end,
	DROP COLUMN IF EXISTS current_usage;

	-- Add indexes for new columns
	CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions(status);
	CREATE INDEX IF NOT EXISTS idx_subscriptions_current_period ON subscriptions(current_period_start, current_period_end);
	CREATE INDEX IF NOT EXISTS idx_subscriptions_stripe_customer ON subscriptions(stripe_customer_id);
	CREATE INDEX IF NOT EXISTS idx_subscriptions_stripe_subscription ON subscriptions(stripe_subscription_id);

	-- Update existing records to have proper current period dates
	UPDATE subscriptions 
	SET 
		current_period_start = COALESCE(current_period_start, created_at),
		current_period_end = COALESCE(current_period_end, created_at + INTERVAL '1 month')
	WHERE current_period_start IS NULL OR current_period_end IS NULL;
	`
	
	_, err := DB.Exec(query)
	return err
}

// revertSubscriptionsTable reverts the subscriptions table changes
func revertSubscriptionsTable() error {
	query := `
	-- Remove indexes
	DROP INDEX IF EXISTS idx_subscriptions_stripe_subscription;
	DROP INDEX IF EXISTS idx_subscriptions_stripe_customer;
	DROP INDEX IF EXISTS idx_subscriptions_current_period;
	DROP INDEX IF EXISTS idx_subscriptions_status;

	-- Add back old columns
	ALTER TABLE subscriptions 
	ADD COLUMN IF NOT EXISTS billing_period_start DATE,
	ADD COLUMN IF NOT EXISTS billing_period_end DATE,
	ADD COLUMN IF NOT EXISTS current_usage INTEGER DEFAULT 0;

	-- Remove new columns
	ALTER TABLE subscriptions 
	DROP COLUMN IF EXISTS stripe_subscription_id,
	DROP COLUMN IF EXISTS stripe_customer_id,
	DROP COLUMN IF EXISTS price_per_call,
	DROP COLUMN IF EXISTS current_period_end,
	DROP COLUMN IF EXISTS current_period_start,
	DROP COLUMN IF EXISTS status;
	`
	
	_, err := DB.Exec(query)
	return err
}

// addAdminRole adds is_admin column to users table
func addAdminRole() error {
	query := `
	ALTER TABLE users 
	ADD COLUMN IF NOT EXISTS is_admin BOOLEAN DEFAULT FALSE;

	-- Create index for admin queries
	CREATE INDEX IF NOT EXISTS idx_users_is_admin ON users(is_admin);
	
	-- Set first user as admin if no admins exist
	UPDATE users 
	SET is_admin = TRUE 
	WHERE id = (SELECT MIN(id) FROM users) 
	AND NOT EXISTS (SELECT 1 FROM users WHERE is_admin = TRUE);
	`
	
	_, err := DB.Exec(query)
	return err
}

// removeAdminRole removes is_admin column from users table
func removeAdminRole() error {
	query := `
	DROP INDEX IF EXISTS idx_users_is_admin;
	ALTER TABLE users DROP COLUMN IF EXISTS is_admin;
	`
	
	_, err := DB.Exec(query)
	return err
}

// createOhioAddressesTable creates the ohio_addresses table and loads data from GeoJSON files
func createOhioAddressesTable() error {
	// First enable PostGIS extension if not already enabled
	if _, err := DB.Exec("CREATE EXTENSION IF NOT EXISTS postgis"); err != nil {
		return fmt.Errorf("failed to enable PostGIS extension: %w", err)
	}

	// Create the table with PostGIS geometry support
	createTableQuery := `
	-- Create ohio_addresses table with PostGIS geometry
	CREATE TABLE IF NOT EXISTS ohio_addresses (
		id BIGSERIAL PRIMARY KEY,
		hash VARCHAR(255) UNIQUE NOT NULL,
		house_number VARCHAR(50),
		street VARCHAR(255),
		unit VARCHAR(50),
		city VARCHAR(255),
		district VARCHAR(10), -- County abbreviation
		region VARCHAR(2), -- State code
		postcode VARCHAR(10),
		county VARCHAR(255), -- Full county name from filename
		geom GEOMETRY(POINT, 4326) NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Create spatial index for better query performance
	CREATE INDEX IF NOT EXISTS idx_ohio_addresses_geom ON ohio_addresses USING GIST (geom);
	
	-- Create indexes for common queries
	CREATE INDEX IF NOT EXISTS idx_ohio_addresses_hash ON ohio_addresses(hash);
	CREATE INDEX IF NOT EXISTS idx_ohio_addresses_county ON ohio_addresses(county);
	CREATE INDEX IF NOT EXISTS idx_ohio_addresses_district ON ohio_addresses(district);
	CREATE INDEX IF NOT EXISTS idx_ohio_addresses_city ON ohio_addresses(city);
	CREATE INDEX IF NOT EXISTS idx_ohio_addresses_postcode ON ohio_addresses(postcode);
	CREATE INDEX IF NOT EXISTS idx_ohio_addresses_street ON ohio_addresses(street);
	`
	
	if _, err := DB.Exec(createTableQuery); err != nil {
		return fmt.Errorf("failed to create ohio_addresses table: %w", err)
	}

	// Load data from GeoJSON files
	return loadOhioAddressData()
}

// dropOhioAddressesTable drops the ohio_addresses table
func dropOhioAddressesTable() error {
	_, err := DB.Exec("DROP TABLE IF EXISTS ohio_addresses")
	return err
}

// loadOhioAddressData loads address data from all Ohio county GeoJSON files
func loadOhioAddressData() error {
	log.Println("Loading Ohio address data from GeoJSON files...")
	
	// Get all GeoJSON files in the oh directory
	files, err := filepath.Glob("oh/*-addresses-county.geojson")
	if err != nil {
		return fmt.Errorf("failed to find GeoJSON files: %w", err)
	}

	totalRecords := 0
	for _, filePath := range files {
		// Extract county name from filename
		filename := filepath.Base(filePath)
		countyName := strings.TrimSuffix(filename, "-addresses-county.geojson")
		countyName = strings.ReplaceAll(countyName, "_", " ")
		countyName = strings.ReplaceAll(countyName, "-", " ")
		countyName = strings.Title(strings.ToLower(strings.TrimSpace(countyName)))

		log.Printf("Processing %s (%s)", filename, countyName)
		
		// Open and read the GeoJSON file
		file, err := os.Open(filePath)
		if err != nil {
			log.Printf("Warning: Failed to open %s: %v", filePath, err)
			continue
		}

		scanner := bufio.NewScanner(file)
		batchSize := 1000
		batch := make([]string, 0, batchSize)
		recordCount := 0

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			// Parse the GeoJSON feature
			var feature struct {
				Type       string `json:"type"`
				Properties struct {
					Hash     string `json:"hash"`
					Number   string `json:"number"`
					Street   string `json:"street"`
					Unit     string `json:"unit"`
					City     string `json:"city"`
					District string `json:"district"`
					Region   string `json:"region"`
					Postcode string `json:"postcode"`
					ID       string `json:"id"`
				} `json:"properties"`
				Geometry struct {
					Type        string    `json:"type"`
					Coordinates []float64 `json:"coordinates"`
				} `json:"geometry"`
			}

			if err := json.Unmarshal([]byte(line), &feature); err != nil {
				log.Printf("Warning: Failed to parse JSON in %s: %v", filePath, err)
				continue
			}

			// Skip if not a valid point feature
			if feature.Type != "Feature" || feature.Geometry.Type != "Point" || len(feature.Geometry.Coordinates) != 2 {
				continue
			}

			longitude := feature.Geometry.Coordinates[0]
			latitude := feature.Geometry.Coordinates[1]

			// Prepare the SQL values for batch insert
			values := fmt.Sprintf("('%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', ST_SetSRID(ST_MakePoint(%f, %f), 4326))",
				strings.ReplaceAll(feature.Properties.Hash, "'", "''"),
				strings.ReplaceAll(feature.Properties.Number, "'", "''"),
				strings.ReplaceAll(feature.Properties.Street, "'", "''"),
				strings.ReplaceAll(feature.Properties.Unit, "'", "''"),
				strings.ReplaceAll(feature.Properties.City, "'", "''"),
				strings.ReplaceAll(feature.Properties.District, "'", "''"),
				strings.ReplaceAll(feature.Properties.Region, "'", "''"),
				strings.ReplaceAll(feature.Properties.Postcode, "'", "''"),
				strings.ReplaceAll(countyName, "'", "''"),
				longitude, latitude)
			
			batch = append(batch, values)
			recordCount++

			// Execute batch insert when batch is full
			if len(batch) >= batchSize {
				if err := executeBatchInsert(batch); err != nil {
					log.Printf("Warning: Batch insert failed for %s: %v", filePath, err)
				}
				batch = batch[:0] // Reset batch
			}
		}

		// Execute remaining records in batch
		if len(batch) > 0 {
			if err := executeBatchInsert(batch); err != nil {
				log.Printf("Warning: Final batch insert failed for %s: %v", filePath, err)
			}
		}

		file.Close()
		
		if err := scanner.Err(); err != nil {
			log.Printf("Warning: Error reading %s: %v", filePath, err)
		}

		log.Printf("Loaded %d records from %s", recordCount, countyName)
		totalRecords += recordCount
	}

	log.Printf("Successfully loaded %d total address records from Ohio counties", totalRecords)
	return nil
}

// executeBatchInsert executes a batch insert of address records
func executeBatchInsert(batch []string) error {
	if len(batch) == 0 {
		return nil
	}

	query := `
	INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom)
	VALUES ` + strings.Join(batch, ", ") + `
	ON CONFLICT (hash) DO NOTHING`

	_, err := DB.Exec(query)
	return err
}

// createOhioCountiesTable creates the ohio_counties table and loads boundary data from GeoJSON meta files
func createOhioCountiesTable() error {
	// First create the table with PostGIS geometry support
	createTableQuery := `
	-- Create ohio_counties table with PostGIS geometry
	CREATE TABLE IF NOT EXISTS ohio_counties (
		id SERIAL PRIMARY KEY,
		county_name VARCHAR(255) UNIQUE NOT NULL,
		source_name VARCHAR(255) NOT NULL,
		layer VARCHAR(100) NOT NULL,
		address_count INTEGER DEFAULT 0,
		stats JSONB,
		bounds_geometry GEOMETRY(POLYGON, 4326) NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Create spatial index for better query performance
	CREATE INDEX IF NOT EXISTS idx_ohio_counties_bounds ON ohio_counties USING GIST (bounds_geometry);
	
	-- Create indexes for common queries
	CREATE INDEX IF NOT EXISTS idx_ohio_counties_name ON ohio_counties(county_name);
	CREATE INDEX IF NOT EXISTS idx_ohio_counties_address_count ON ohio_counties(address_count);
	`;
	
	if _, err := DB.Exec(createTableQuery); err != nil {
		return fmt.Errorf("failed to create ohio_counties table: %w", err)
	}

	// Load county boundary data from meta files
	return loadOhioCountyBoundaries()
}

// dropOhioCountiesTable drops the ohio_counties table
func dropOhioCountiesTable() error {
	_, err := DB.Exec("DROP TABLE IF EXISTS ohio_counties")
	return err
}

// loadOhioCountyBoundaries loads county boundary data from all Ohio county GeoJSON meta files
func loadOhioCountyBoundaries() error {
	log.Println("Loading Ohio county boundary data from GeoJSON meta files...")
	
	// Get all meta files in the oh directory (only address county files, not buildings/parcels)
	files, err := filepath.Glob("oh/*-addresses-county.geojson.meta")
	if err != nil {
		return fmt.Errorf("failed to find GeoJSON meta files: %w", err)
	}

	totalRecords := 0
	for _, filePath := range files {
		// Extract county name from filename
		filename := filepath.Base(filePath)
		countyName := strings.TrimSuffix(filename, "-addresses-county.geojson.meta")
		countyName = strings.ReplaceAll(countyName, "_", " ")
		countyName = strings.ReplaceAll(countyName, "-", " ")
		countyName = strings.Title(strings.ToLower(strings.TrimSpace(countyName)))

		log.Printf("Processing county boundary: %s (%s)", filename, countyName)
		
		// Read and parse the meta file
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("Warning: Failed to read %s: %v", filePath, err)
			continue
		}

		var metaData struct {
			SourceName string `json:"source_name"`
			Layer      string `json:"layer"`
			Count      int    `json:"count"`
			Stats      map[string]interface{} `json:"stats"`
			Bounds     struct {
				Type        string    `json:"type"`
				Coordinates [][][]float64 `json:"coordinates"`
			} `json:"bounds"`
		}

		if err := json.Unmarshal(data, &metaData); err != nil {
			log.Printf("Warning: Failed to parse JSON in %s: %v", filePath, err)
			continue
		}

		// Skip if not a valid polygon
		if metaData.Bounds.Type != "Polygon" || len(metaData.Bounds.Coordinates) == 0 {
			log.Printf("Warning: Invalid polygon bounds in %s", filePath)
			continue
		}

		// Convert coordinates to WKT format for PostGIS
		coords := metaData.Bounds.Coordinates[0] // First ring of the polygon
		var wktCoords []string
		for _, coord := range coords {
			if len(coord) >= 2 {
				wktCoords = append(wktCoords, fmt.Sprintf("%f %f", coord[0], coord[1]))
			}
		}
		
		if len(wktCoords) < 4 {
			log.Printf("Warning: Invalid polygon coordinates in %s", filePath)
			continue
		}

		polygonWKT := fmt.Sprintf("POLYGON((%s))", strings.Join(wktCoords, ", "))

		// Convert stats to JSON
		statsJSON, err := json.Marshal(metaData.Stats)
		if err != nil {
			log.Printf("Warning: Failed to marshal stats for %s: %v", filePath, err)
			statsJSON = []byte("{}")
		}

		// Insert county boundary data
		query := `
		INSERT INTO ohio_counties (county_name, source_name, layer, address_count, stats, bounds_geometry)
		VALUES ($1, $2, $3, $4, $5, ST_SetSRID(ST_GeomFromText($6), 4326))
		ON CONFLICT (county_name) DO UPDATE SET
			source_name = EXCLUDED.source_name,
			layer = EXCLUDED.layer,
			address_count = EXCLUDED.address_count,
			stats = EXCLUDED.stats,
			bounds_geometry = EXCLUDED.bounds_geometry,
			updated_at = CURRENT_TIMESTAMP
		`

		_, err = DB.Exec(query, countyName, metaData.SourceName, metaData.Layer, metaData.Count, string(statsJSON), polygonWKT)
		if err != nil {
			log.Printf("Warning: Failed to insert county %s: %v", countyName, err)
			continue
		}

		totalRecords++
	}

	log.Printf("Successfully loaded %d county boundary records", totalRecords)
	
	// Clean up GeoJSON files after successful loading to save disk space
	if err := cleanupGeoJSONFiles(); err != nil {
		log.Printf("Warning: Failed to cleanup GeoJSON files: %v", err)
		// Don't return error as the migration was successful
	}
	
	return nil
}

// cleanupGeoJSONFiles removes GeoJSON and meta files after data has been loaded into database
func cleanupGeoJSONFiles() error {
	log.Println("Cleaning up GeoJSON files to save disk space...")
	
	// Check if we're in production environment
	isProd := os.Getenv("ENV") == "production" || os.Getenv("GO_ENV") == "production"
	
	// Also check if CLEANUP_GEOJSON is explicitly set
	cleanupEnabled := os.Getenv("CLEANUP_GEOJSON") == "true"
	
	if !isProd && !cleanupEnabled {
		log.Println("Skipping GeoJSON cleanup in development environment. Set CLEANUP_GEOJSON=true to force cleanup.")
		return nil
	}
	
	// Get all GeoJSON files (both .geojson and .geojson.meta files)
	patterns := []string{
		"oh/*.geojson",
		"oh/*.geojson.meta",
	}
	
	totalFilesDeleted := 0
	var totalSizeFreed int64
	
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			log.Printf("Warning: Failed to find files with pattern %s: %v", pattern, err)
			continue
		}
		
		for _, filePath := range files {
			// Get file size before deletion
			if info, err := os.Stat(filePath); err == nil {
				totalSizeFreed += info.Size()
			}
			
			// Delete the file
			if err := os.Remove(filePath); err != nil {
				log.Printf("Warning: Failed to delete %s: %v", filePath, err)
				continue
			}
			
			totalFilesDeleted++
		}
	}
	
	// Convert bytes to human readable format
	sizeFreedMB := float64(totalSizeFreed) / (1024 * 1024)
	
	log.Printf("Successfully cleaned up %d GeoJSON files, freed %.2f MB of disk space", 
		totalFilesDeleted, sizeFreedMB)
	
	// Remove the oh directory if it's empty
	if entries, err := os.ReadDir("oh"); err == nil && len(entries) == 0 {
		if err := os.Remove("oh"); err != nil {
			log.Printf("Warning: Failed to remove empty oh directory: %v", err)
		} else {
			log.Println("Removed empty oh directory")
		}
	}
	
	return nil
}