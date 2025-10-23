package database

import (
	"fmt"
	"log"
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