package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"geocoding-api/database"
	"geocoding-api/handlers"
	"geocoding-api/middleware"
	"geocoding-api/services"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
)

func main() {
	// Load environment variables from .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}
	
	// Warn about insecure defaults in production
	if os.Getenv("GO_ENV") == "production" {
		if os.Getenv("JWT_SECRET") == "change_this_in_production" || os.Getenv("JWT_SECRET") == "" {
			log.Println("WARNING: Using default JWT_SECRET in production! Set a secure value.")
		}
		if os.Getenv("API_SECRET_KEY") == "change_this_in_production" || os.Getenv("API_SECRET_KEY") == "" {
			log.Println("WARNING: Using default API_SECRET_KEY in production! Set a secure value.")
		}
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

	// Initialize services
	services.InitAddressService(database.DB)
	
	// Initialize ZIP code data if needed
	if err := services.InitializeData(); err != nil {
		log.Printf("Warning: Failed to initialize ZIP code data: %v", err)
		log.Println("You can load data manually using: curl -X POST http://localhost:8080/api/v1/admin/load-data")
	}
	
	// Initialize Ohio address data if needed
	if err := services.InitializeOhioData(); err != nil {
		log.Printf("Warning: Failed to initialize Ohio address data: %v", err)
		log.Println("Ohio addresses can be loaded manually if needed")
	}

	// Create Echo instance
	e := echo.New()

	// Middleware
	e.Use(echomiddleware.Logger())
	e.Use(echomiddleware.Recover())
	
	// Configure CORS based on environment
	var corsOrigins []string
	
	// Check for custom CORS origins from environment
	if customOrigins := os.Getenv("CORS_ORIGINS"); customOrigins != "" {
		corsOrigins = strings.Split(customOrigins, ",")
		for i, origin := range corsOrigins {
			corsOrigins[i] = strings.TrimSpace(origin)
		}
		log.Printf("Using custom CORS origins: %v", corsOrigins)
	} else if os.Getenv("GO_ENV") == "production" {
		// Production defaults
		corsOrigins = []string{
			"https://geocode.jfay.dev",
			"https://www.geocode.jfay.dev",
		}
		log.Printf("Using production CORS origins: %v", corsOrigins)
	} else {
		// Development mode - allow localhost variants
		corsOrigins = []string{
			"http://localhost:8080",
			"http://127.0.0.1:8080",
			"http://localhost:3000", // Common dev ports
			"http://localhost:3001",
		}
		log.Printf("Using development CORS origins: %v", corsOrigins)
	}
	
	e.Use(echomiddleware.CORSWithConfig(echomiddleware.CORSConfig{
		AllowOrigins: corsOrigins,
		AllowMethods: []string{echo.GET, echo.POST, echo.PUT, echo.DELETE, echo.OPTIONS},
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			echo.HeaderAuthorization,
			"X-API-Key",
			"X-User-ID",
		},
		AllowCredentials: true,
		MaxAge:          300, // 5 minutes
	}))

	// Add request ID middleware for tracing
	e.Use(echomiddleware.RequestID())

	// Static files for web interface
	e.Static("/static", "static")
	
	// Web interface routes
	e.GET("/", func(c echo.Context) error {
		return c.File("static/index.html")
	})
	e.GET("/auth/signin", func(c echo.Context) error {
		return c.File("static/signin.html")
	})
	e.GET("/auth/signup", func(c echo.Context) error {
		return c.File("static/signup.html")
	})
	e.GET("/dashboard", func(c echo.Context) error {
		return c.File("static/dashboard.html")
	})
	e.GET("/admin", func(c echo.Context) error {
		return c.File("static/admin.html")
	})
	
	// Documentation routes
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
	
	// Health check endpoint (no auth required)
	api.GET("/health", handlers.HealthCheckHandler)
	
	// Authentication routes (no auth required)
	auth := api.Group("/auth")
	auth.POST("/register", handlers.RegisterHandler)
	auth.POST("/login", handlers.LoginHandler)
	auth.GET("/plans", handlers.GetPlansHandler)
	
	// User management routes (require user auth)
	user := api.Group("/user")
	user.Use(middleware.RequireUserAuth())
	user.POST("/api-keys", handlers.CreateAPIKeyHandler)
	user.GET("/api-keys", handlers.GetAPIKeysHandler)
	user.DELETE("/api-keys/:id", handlers.DeleteAPIKeyHandler)
	user.GET("/usage", handlers.GetUsageHandler)
	
	// Protected API endpoints (require API key)
	protected := api.Group("")
	protected.Use(middleware.APIKeyAuth())
	protected.Use(middleware.UsageHeader())
	
	// Geocoding endpoints
	protected.GET("/geocode/:zipcode", handlers.GetZipCodeHandler)
	protected.GET("/search", handlers.SearchZipCodesHandler)
	
	// Distance and proximity endpoints
	protected.GET("/distance/:from/:to", handlers.CalculateDistanceHandler)
	protected.GET("/nearby/:zipcode", handlers.FindNearbyZipCodesHandler)
	protected.GET("/proximity/:center/:target", handlers.CheckZipCodeProximityHandler)
	
	// Ohio address endpoints
	protected.GET("/addresses", handlers.SearchOhioAddressesHandler)
	protected.GET("/addresses/semantic", handlers.SemanticSearchAddressesHandler)
	protected.GET("/addresses/:id", handlers.GetOhioAddressHandler)
	
	// Ohio county boundary endpoints
	protected.GET("/counties", handlers.GetCountiesHandler)
	protected.GET("/counties/:name", handlers.GetCountyDetailHandler)
	protected.GET("/counties/:name/boundary", handlers.GetCountyBoundaryHandler)
	protected.GET("/counties/bounds/search", handlers.GetCountiesInBoundsHandler)
	
	// Admin routes (require admin auth)
	admin := api.Group("/admin")
	admin.Use(middleware.RequireAdminAuth())
	admin.GET("/user/status", handlers.GetUserStatusHandler)
	admin.POST("/load-data", handlers.LoadDataHandler)
	admin.GET("/stats", handlers.GetAdminStatsHandler)
	admin.GET("/users", handlers.GetAllUsersHandler)
	admin.PUT("/users/:id/status", handlers.UpdateUserStatusHandler)
	admin.PUT("/users/:id/admin", handlers.UpdateUserAdminHandler)
	admin.GET("/api-keys", handlers.GetAllAPIKeysHandler)
	admin.GET("/system-status", handlers.GetSystemStatusHandler)
	admin.GET("/counties", handlers.GetCountyStatsHandler)

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