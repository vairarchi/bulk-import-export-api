package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"bulk-import-export-api/internal/handlers"
	"bulk-import-export-api/internal/storage"
	"bulk-import-export-api/pkg/jobs"
	"bulk-import-export-api/pkg/streaming"
)

func main() {
	// Load configuration from environment variables
	config := loadConfig()

	// Initialize database connection
	db, err := initDatabase(config.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize storage
	store := storage.NewStorage(db)
	if err := store.InitSchema(); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Create required directories
	createDirectories(config.UploadsDir, config.ExportsDir)

	// Initialize components
	jobManager := jobs.NewJobManager()
	idempotencyMgr := jobs.NewIdempotencyManager()
	streamProcessor := streaming.NewProcessor(store, jobManager, config.ExportsDir)
	jobProcessor := jobs.NewJobProcessor(jobManager, store, streamProcessor)

	// Initialize handlers
	handler := handlers.NewHandler(
		jobManager,
		jobProcessor,
		streamProcessor,
		idempotencyMgr,
		config.UploadsDir,
		config.ExportsDir,
	)

	// Setup Gin router
	router := setupRouter(handler)

	// Start cleanup routine
	go startCleanupRoutine(jobManager, idempotencyMgr)

	// Start server
	log.Printf("Starting server on %s", config.ServerAddress)
	log.Printf("Uploads directory: %s", config.UploadsDir)
	log.Printf("Exports directory: %s", config.ExportsDir)
	log.Printf("Database: %s", maskDBURL(config.DatabaseURL))

	if err := router.Run(config.ServerAddress); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// Config holds application configuration
type Config struct {
	ServerAddress string
	DatabaseURL   string
	UploadsDir    string
	ExportsDir    string
}

// loadConfig loads configuration from environment variables with defaults
func loadConfig() *Config {
	return &Config{
		ServerAddress: getEnv("SERVER_ADDRESS", ":8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://user:password@localhost/bulk_api?sslmode=disable"),
		UploadsDir:    getEnv("UPLOADS_DIR", "./uploads"),
		ExportsDir:    getEnv("EXPORTS_DIR", "./exports"),
	}
}

// getEnv gets environment variable with default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// initDatabase initializes the database connection
func initDatabase(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// createDirectories creates required directories if they don't exist
func createDirectories(dirs ...string) {
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
}

// setupRouter configures the Gin router with all routes and middleware
func setupRouter(handler *handlers.Handler) *gin.Engine {
	// Set Gin mode based on environment
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Global middleware
	router.Use(gin.Recovery())
	router.Use(handler.RequestLogger())
	router.Use(handler.CORS())
	router.Use(handler.RateLimit())
	router.Use(handler.RequestSizeLimit())

	// Health check endpoint
	router.GET("/health", handler.HealthCheck)

	// Metrics endpoint for Prometheus
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API v1 routes
	v1 := router.Group("/v1")
	{
		// Import endpoints
		imports := v1.Group("/imports")
		{
			imports.POST("", handler.CreateImportJob)
			imports.GET("/:job_id", handler.GetImportJob)
		}

		// Export endpoints
		exports := v1.Group("/exports")
		{
			// Streaming export
			exports.GET("", handler.StreamExport)
			// Async export
			exports.POST("", handler.CreateExportJob)
			exports.GET("/:job_id", handler.GetExportJob)
		}

		// Admin endpoints
		admin := v1.Group("/admin")
		{
			admin.GET("/stats", handler.GetJobStats)
		}
	}

	// Static file serving for downloads
	router.Static("/downloads", "./exports")

	// 404 handler
	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Endpoint not found",
			"path":  c.Request.URL.Path,
		})
	})

	return router
}

// startCleanupRoutine starts background cleanup of old jobs and files
func startCleanupRoutine(jobManager *jobs.JobManager, idempotencyMgr *jobs.IdempotencyManager) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Clean up jobs older than 24 hours
			jobManager.CleanupOldJobs(24 * time.Hour)

			// Clean up idempotency keys older than 1 hour
			idempotencyMgr.CleanupIdempotencyKeys(1 * time.Hour)

			// Clean up old export files (older than 7 days)
			cleanupOldFiles("./exports", 7*24*time.Hour)

			// Clean up old upload files (older than 1 day)
			cleanupOldFiles("./uploads", 24*time.Hour)

			log.Println("Cleanup completed")
		}
	}
}

// cleanupOldFiles removes files older than the specified duration
func cleanupOldFiles(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("Failed to read directory %s: %v", dir, err)
		return
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			filePath := fmt.Sprintf("%s/%s", dir, entry.Name())
			if err := os.Remove(filePath); err == nil {
				removed++
			}
		}
	}

	if removed > 0 {
		log.Printf("Cleaned up %d old files from %s", removed, dir)
	}
}

// maskDBURL masks sensitive information in database URL for logging
func maskDBURL(dbURL string) string {
	// Simple masking - in production, use a proper URL parsing library
	if len(dbURL) > 20 {
		return dbURL[:10] + "***" + dbURL[len(dbURL)-7:]
	}
	return "***"
}
