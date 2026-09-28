package main

import (
	"time"

	"geocoding-api/handlers"
	"geocoding-api/middleware"

	"github.com/labstack/echo/v4"
)

// registerRoutes wires every route onto e.
//
// Lifted out of main so a test can walk the routing table: which endpoints
// exist, and which scope each one demands, is a question worth being able to
// ask in a test rather than by reading four hundred lines of startup.
// staticDir is where the built frontend lives, chosen by main at startup.
func registerRoutes(e *echo.Echo, staticDir string) {
	// Routes
	api := e.Group("/api/v1")

	// Health check endpoint (no auth required)
	api.GET("/health", handlers.HealthCheckHandler)

	// Authentication routes (no auth required)
	//
	// These are the only endpoints reachable without a key, so they are the
	// only ones the monthly quota in APIKeyAuth does not cover. Login runs a
	// bcrypt comparison per attempt, which makes an unthrottled endpoint both
	// a credential-stuffing surface and a cheap way to burn server CPU.
	auth := api.Group("/auth")
	// Applied per route, not to the group. /plans is a cheap public pricing
	// lookup with no bcrypt behind it; sharing a bucket with login means a
	// pricing page that refetches, or a few users behind one corporate NAT,
	// could exhaust the allowance and lock people out of signing in.
	authThrottle := middleware.AuthRateLimiter()
	auth.POST("/register", handlers.RegisterHandler, authThrottle)
	auth.POST("/login", handlers.LoginHandler, authThrottle)
	auth.GET("/plans", handlers.GetPlansHandler)

	// User management routes (require user auth)
	user := api.Group("/user")
	user.Use(middleware.RequireUserAuth())
	user.GET("/profile", handlers.GetUserProfileHandler)
	user.POST("/api-keys", handlers.CreateAPIKeyHandler)
	user.GET("/api-keys", handlers.GetAPIKeysHandler)
	user.DELETE("/api-keys/:id", handlers.DeleteAPIKeyHandler)
	user.POST("/api-keys/:id/roll", handlers.RollAPIKeyHandler)
	user.PUT("/api-keys/:id/limits", handlers.SetAPIKeyLimitsHandler)
	user.GET("/usage", handlers.GetUsageHandler)
	user.GET("/usage/daily", handlers.GetDailyUsageHandler)
	user.GET("/usage/endpoints", handlers.GetEndpointUsageHandler)
	user.GET("/usage/keys", handlers.GetKeyUsageHandler)

	// Protected API endpoints (require API key)
	//
	// Wrapped in a recorder so a test can ask which routes APIKeyAuth guards
	// instead of guessing from their paths. Guessing was wrong in a way that
	// mattered: a protected route registered under /user or /admin looked
	// exempt, and so escaped the check that every guarded route declares a
	// permission scope.
	protected := &recordingGroup{group: api.Group("")}
	protected.Use(middleware.APIKeyAuth())
	protected.Use(middleware.UsageHeader())

	// What data the service actually holds. Answering this without it means
	// querying a state and inferring from an empty result, which is
	// indistinguishable from a broken query.
	protected.GET("/coverage", handlers.GetCoverageHandler)

	// Geocoding endpoints
	protected.GET("/geocode/:zipcode", handlers.GetZipCodeHandler)
	protected.GET("/reverse", handlers.ReverseGeocodeHandler)
	protected.GET("/enrich", handlers.EnrichHandler)
	protected.POST("/geocode/batch", handlers.BatchGeocodeHandler)
	protected.POST("/address/validate", handlers.ValidateAddressHandler)
	protected.GET("/search", handlers.SearchZipCodesHandler)

	// Distance and proximity endpoints
	protected.GET("/distance/:from/:to", handlers.CalculateDistanceHandler)
	protected.GET("/nearby/:zipcode", handlers.FindNearbyZipCodesHandler)
	protected.GET("/proximity/:center/:target", handlers.CheckZipCodeProximityHandler)

	// Ohio address endpoints
	protected.GET("/addresses", handlers.SearchOhioAddressesHandler)
	protected.GET("/addresses/search", handlers.FullTextSearchAddressesHandler)
	protected.GET("/addresses/:id", handlers.GetOhioAddressHandler)

	// Vector tiles. Cacheable for the same reason the boundary endpoints are:
	// every key gets byte-identical census geometry for a given URL, and it
	// changes about once a year.
	protected.GET("/tiles/:layer/:z/:x/:y", handlers.GetTileHandler, middleware.CacheStatic(24*time.Hour))

	// Ohio county boundary endpoints
	protected.GET("/counties", handlers.GetCountiesHandler)
	protected.GET("/counties/:name", handlers.GetCountyDetailHandler)
	protected.GET("/counties/:name/boundary", handlers.GetCountyBoundaryHandler, middleware.CacheStatic(24*time.Hour))
	protected.GET("/counties/bounds/search", handlers.GetCountiesInBoundsHandler)

	// City endpoints
	protected.GET("/cities", handlers.SearchCitiesHandler)
	protected.GET("/cities/:id", handlers.GetCityHandler)
	protected.GET("/cities/zips", handlers.GetCityZIPCodesHandler)

	// State endpoints
	protected.GET("/states", handlers.SearchStatesHandler)
	protected.GET("/states/lookup", handlers.GetStateByLocationHandler)
	protected.GET("/states/:identifier", handlers.GetStateHandler)
	protected.GET("/states/:identifier/boundary", handlers.GetStateBoundaryHandler, middleware.CacheStatic(24*time.Hour))

	// Admin routes (require admin auth)
	admin := api.Group("/admin")
	admin.Use(middleware.RequireAdminAuth())
	admin.GET("/user/status", handlers.GetUserStatusHandler)
	admin.POST("/load-data", handlers.LoadDataHandler)
	admin.GET("/stats", handlers.GetAdminStatsHandler)
	admin.GET("/users", handlers.GetAllUsersHandler)
	admin.GET("/users/:id/metrics", handlers.GetUserUsageMetricsHandler)
	admin.PUT("/users/:id/status", handlers.UpdateUserStatusHandler)
	admin.PUT("/users/:id/admin", handlers.UpdateUserAdminHandler)
	admin.GET("/api-keys", handlers.GetAllAPIKeysHandler)
	admin.GET("/system-status", handlers.GetSystemStatusHandler)
	admin.GET("/data-quality", handlers.GetDataQualityHandler)
	admin.POST("/usage-counters/rebuild", handlers.RebuildUsageCountersHandler)
	admin.GET("/counties", handlers.GetCountyStatsHandler)
	admin.POST("/counties/load", handlers.LoadCountyBoundariesHandler)
	admin.GET("/boundaries", handlers.GetBoundaryLoadsHandler)
	admin.POST("/boundaries/load", handlers.LoadBoundariesHandler)
	admin.GET("/analytics", handlers.GetAdminAnalyticsHandler)

	// Dataset management routes (admin only)
	admin.POST("/datasets/upload", handlers.UploadDatasetHandler)
	admin.POST("/datasets/upload-bulk", handlers.UploadMultipleHandler)
	admin.POST("/datasets/upload-bulk-stream", handlers.UploadMultipleStreamHandler)
	admin.GET("/datasets", handlers.GetDatasetsHandler)
	admin.GET("/datasets/stats", handlers.GetDatasetStatsHandler)
	admin.GET("/datasets/:id", handlers.GetDatasetHandler)
	admin.POST("/datasets/:id/reprocess", handlers.ReprocessDatasetHandler)
	admin.DELETE("/datasets/:id", handlers.DeleteDatasetHandler)

	// SPA fallback - MUST be registered AFTER all API routes
	// This serves the React app for all non-API routes
	e.GET("/*", spaHandler(staticDir))

}

// protectedRoutePaths is every route the protected group registered, in
// registration order. Read by the test that checks each one declares a scope.
var protectedRoutePaths []string

// recordingGroup registers routes on a group and remembers their full paths.
// It carries only the methods the protected group uses; a new verb has to be
// added here, which is the point -- an unrecorded route is an unchecked one.
type recordingGroup struct {
	group *echo.Group
}

func (r *recordingGroup) Use(m ...echo.MiddlewareFunc) { r.group.Use(m...) }

func (r *recordingGroup) GET(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) {
	r.record(path)
	r.group.GET(path, h, m...)
}

func (r *recordingGroup) POST(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) {
	r.record(path)
	r.group.POST(path, h, m...)
}

func (r *recordingGroup) record(path string) {
	protectedRoutePaths = append(protectedRoutePaths, "/api/v1"+path)
}
