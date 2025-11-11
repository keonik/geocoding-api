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