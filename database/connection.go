package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
)

// DB holds the database connection
var DB *sql.DB

// InitDB initializes the database connection with retry logic
func InitDB() error {
	host := getEnv("DB_HOST", "localhost")
	port := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "postgres")
	password := getEnv("DB_PASSWORD", "postgres")
	dbname := getEnv("DB_NAME", "geocoding_db")
	sslmode := getEnv("DB_SSLMODE", "disable")
	
	psqlInfo := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)
	
	maskedUrl := fmt.Sprintf("postgres://%s:***@%s:%s/%s?sslmode=%s", user, host, port, dbname, sslmode)
	log.Printf("Connecting to database: %s", maskedUrl)

	var err error
	
	// Retry logic for database connection (useful for container startup ordering)
	maxRetries := 30
	retryDelay := 2 * time.Second
	
	for i := 0; i < maxRetries; i++ {
		DB, err = sql.Open("postgres", psqlInfo)
		if err != nil {
			log.Printf("Attempt %d/%d: Failed to open database: %v", i+1, maxRetries, err)
			time.Sleep(retryDelay)
			continue
		}

		err = DB.Ping()
		if err == nil {
			break
		}
		
		log.Printf("Attempt %d/%d: Failed to ping database: %v", i+1, maxRetries, err)
		DB.Close()
		time.Sleep(retryDelay)
	}
	
	if err != nil {
		return fmt.Errorf("failed to connect to database after %d attempts: %w", maxRetries, err)
	}

	// Optimize connection pool for performance
	DB.SetMaxOpenConns(25)          // Maximum open connections
	DB.SetMaxIdleConns(10)           // Keep connections ready
	DB.SetConnMaxLifetime(0)         // Reuse connections indefinitely

	log.Println("Database connection established successfully")
	return nil
}

// CreateTables creates the necessary database tables (deprecated - use RunMigrations instead)
func CreateTables() error {
	log.Println("CreateTables is deprecated, using RunMigrations instead")
	return RunMigrations()
}

// CloseDB closes the database connection
func CloseDB() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}

// getEnv gets an environment variable with a default fallback
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}