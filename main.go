package main

import (
	"log"
	"net/http"
	"os"

	"geocoding-api/database"
	"geocoding-api/handlers"
	"geocoding-api/services"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// Load environment variables from .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// Initialize database connection
	if err := database.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.CloseDB()

	// Run database migrations
	if err := database.RunMigrations(); err != nil {
		log.Fatalf("Failed to run database migrations: %v", err)
	}

	// Initialize data if needed
	if err := services.InitializeData(); err != nil {
		log.Printf("Warning: Failed to initialize data: %v", err)
		log.Println("You can load data manually using: curl -X POST http://localhost:8080/api/v1/admin/load-data")
	}

	// Create Echo instance
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Add request ID middleware for tracing
	e.Use(middleware.RequestID())

	// Documentation routes
	e.GET("/", handlers.DocsRedirectHandler)
	e.Static("/docs", "docs")
	
	// Serve OpenAPI spec in multiple formats
	e.File("/api-docs.yaml", "api-docs.yaml")
	e.GET("/openapi.yaml", func(c echo.Context) error {
		return c.File("api-docs.yaml")
	})
	e.GET("/swagger.yaml", func(c echo.Context) error {
		return c.File("api-docs.yaml")
	})
	e.GET("/spec", func(c echo.Context) error {
		return c.File("api-docs.yaml")
	})
	
	// Serve spec as JSON (note: most tools accept YAML)
	e.GET("/api-docs.json", func(c echo.Context) error {
		c.Response().Header().Set("Content-Type", "application/json")
		return c.JSON(http.StatusOK, map[string]interface{}{
			"message": "OpenAPI spec is available in YAML format at /api-docs.yaml",
			"yaml_url": "/api-docs.yaml",
			"note": "Most tools (including Scalar) work perfectly with YAML specs",
		})
	})
	e.GET("/openapi.json", func(c echo.Context) error {
		return c.Redirect(http.StatusPermanentRedirect, "/api-docs.json")
	})
	
	// Discovery endpoint for API information
	e.GET("/api-docs-test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"status": "API docs are accessible",
			"documentation": map[string]string{
				"interactive_docs": "/docs",
				"advanced_docs":    "/docs/advanced.html",
				"fallback_docs":    "/docs/fallback.html",
			},
			"specifications": map[string]string{
				"yaml":        "/api-docs.yaml",
				"openapi":     "/openapi.yaml", 
				"swagger":     "/swagger.yaml",
				"spec":        "/spec",
				"json":        "/api-docs.json",
				"openapi_json": "/openapi.json",
			},
			"server": c.Request().Host,
		})
	})

	// Routes
	api := e.Group("/api/v1")
	
	// Health check endpoint
	api.GET("/health", handlers.HealthCheckHandler)
	
	// Geocoding endpoints
	api.GET("/geocode/:zipcode", handlers.GetZipCodeHandler)
	api.GET("/search", handlers.SearchZipCodesHandler)
	
	// Distance and proximity endpoints
	api.GET("/distance/:from/:to", handlers.CalculateDistanceHandler)
	api.GET("/nearby/:zipcode", handlers.FindNearbyZipCodesHandler)
	api.GET("/proximity/:center/:target", handlers.CheckZipCodeProximityHandler)
	
	// Admin endpoint for loading data (consider adding authentication in production)
	api.POST("/admin/load-data", handlers.LoadDataHandler)

	// Get port from environment variable or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Start server
	log.Printf("Starting server on port %s", port)
	if err := e.Start(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}