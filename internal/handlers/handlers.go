package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vairarchi/bulk-import-export-api/internal/models"
	"github.com/vairarchi/bulk-import-export-api/pkg/jobs"
	"github.com/vairarchi/bulk-import-export-api/pkg/streaming"
)

// Handler handles HTTP requests for import/export operations
type Handler struct {
	jobManager      *jobs.JobManager
	jobProcessor    *jobs.JobProcessor
	streamProcessor *streaming.Processor
	idempotencyMgr  *jobs.IdempotencyManager
	uploadsDir      string
	exportDir       string
	maxFileSize     int64
}

// NewHandler creates a new HTTP handler
func NewHandler(
	jobManager *jobs.JobManager,
	jobProcessor *jobs.JobProcessor,
	streamProcessor *streaming.Processor,
	idempotencyMgr *jobs.IdempotencyManager,
	uploadsDir, exportDir string,
) *Handler {
	return &Handler{
		jobManager:      jobManager,
		jobProcessor:    jobProcessor,
		streamProcessor: streamProcessor,
		idempotencyMgr:  idempotencyMgr,
		uploadsDir:      uploadsDir,
		exportDir:       exportDir,
		maxFileSize:     100 * 1024 * 1024, // 100MB max file size
	}
}

// CreateImportJob creates a new import job
func (h *Handler) CreateImportJob(c *gin.Context) {
	// Check idempotency key
	idempotencyKey := c.GetHeader("Idempotency-Key")
	if idempotencyKey != "" {
		if jobID, exists := h.idempotencyMgr.CheckIdempotency(idempotencyKey); exists {
			job, found := h.jobManager.GetImportJob(jobID)
			if found {
				c.JSON(http.StatusOK, gin.H{
					"job_id":  jobID,
					"status":  job.Status,
					"message": "Job already exists for this idempotency key",
				})
				return
			}
		}
	}

	var filePath string
	var format string
	var resourceType string

	// Check content type for multipart upload
	contentType := c.GetHeader("Content-Type")
	if contentType != "" && strings.HasPrefix(contentType, "multipart/form-data") {
		// Handle multipart file upload
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to get file from request"})
			return
		}
		defer file.Close()

		// Validate file size
		if header.Size > h.maxFileSize {
			c.JSON(http.StatusBadRequest, gin.H{"error": "File size exceeds maximum allowed size"})
			return
		}

		// Get additional form parameters
		resourceType = c.PostForm("resource_type")
		format = c.PostForm("format")

		// Validate required parameters
		if resourceType == "" || format == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "resource_type and format are required"})
			return
		}

		// Save uploaded file
		fileName := fmt.Sprintf("%d_%s", time.Now().Unix(), header.Filename)
		filePath = filepath.Join(h.uploadsDir, fileName)

		dst, err := os.Create(filePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save uploaded file"})
			return
		}
		defer dst.Close()

		_, err = io.Copy(dst, file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save uploaded file"})
			return
		}
	} else {
		// Handle JSON request with file URL
		var req models.ImportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		resourceType = req.ResourceType
		format = req.Format

		// Download file from URL
		if req.FileURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file_url is required for JSON requests"})
			return
		}

		var err error
		filePath, err = h.downloadFile(req.FileURL)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to download file: %v", err)})
			return
		}
	}

	// Validate resource type and format combination
	if !h.isValidResourceFormat(resourceType, format) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid format '%s' for resource type '%s'", format, resourceType),
		})
		return
	}

	// Create import job
	job := h.jobManager.CreateImportJob(resourceType, filepath.Base(filePath))

	// Set idempotency mapping if provided
	if idempotencyKey != "" {
		h.idempotencyMgr.SetIdempotency(idempotencyKey, job.ID)
	}

	// Start processing asynchronously
	ctx := context.Background()
	go h.jobProcessor.ProcessImportJob(ctx, job.ID, filePath, format)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":  job.ID,
		"status":  job.Status,
		"message": "Import job created successfully",
	})
}

// GetImportJob retrieves the status of an import job
func (h *Handler) GetImportJob(c *gin.Context) {
	jobID := c.Param("job_id")

	job, exists := h.jobManager.GetImportJob(jobID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// StreamExport handles streaming export requests
func (h *Handler) StreamExport(c *gin.Context) {
	resourceType := c.Query("resource")
	format := c.DefaultQuery("format", "ndjson")

	// Validate parameters
	if resourceType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource parameter is required"})
		return
	}

	if !h.isValidResourceFormat(resourceType, format) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid format '%s' for resource type '%s'", format, resourceType),
		})
		return
	}

	// Parse filters from query parameters
	filters := make(map[string]string)
	for key, values := range c.Request.URL.Query() {
		if key != "resource" && key != "format" && len(values) > 0 {
			filters[key] = values[0]
		}
	}

	// Stream the export
	err := h.streamProcessor.StreamExport(c.Writer, resourceType, format, filters)
	if err != nil {
		// If headers haven't been written yet, we can still return a JSON error
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Export failed: %v", err)})
		return
	}
}

// CreateExportJob creates a new asynchronous export job
func (h *Handler) CreateExportJob(c *gin.Context) {
	var req models.ExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate resource type and format
	if !h.isValidResourceFormat(req.ResourceType, req.Format) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid format '%s' for resource type '%s'", req.Format, req.ResourceType),
		})
		return
	}

	// Create export job
	job := h.jobManager.CreateExportJob(req.ResourceType, req.Format, req.Filters)

	// Start processing asynchronously
	ctx := context.Background()
	go h.jobProcessor.ProcessExportJob(ctx, job.ID)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":  job.ID,
		"status":  job.Status,
		"message": "Export job created successfully",
	})
}

// GetExportJob retrieves the status of an export job
func (h *Handler) GetExportJob(c *gin.Context) {
	jobID := c.Param("job_id")

	job, exists := h.jobManager.GetExportJob(jobID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// DownloadExportFile serves export files for download
func (h *Handler) DownloadExportFile(c *gin.Context) {
	fileName := c.Param("filename")
	filePath := filepath.Join(h.exportDir, fileName)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	// Serve the file
	c.File(filePath)
}

// GetJobStats returns statistics about jobs
func (h *Handler) GetJobStats(c *gin.Context) {
	stats := h.jobManager.GetJobStats()
	c.JSON(http.StatusOK, stats)
}

// HealthCheck endpoint
func (h *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
	})
}

// downloadFile downloads a file from URL and saves it locally
func (h *Handler) downloadFile(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download file: HTTP %d", resp.StatusCode)
	}

	// Check content length
	if resp.ContentLength > h.maxFileSize {
		return "", fmt.Errorf("file size exceeds maximum allowed size")
	}

	// Create local file
	fileName := fmt.Sprintf("download_%d", time.Now().Unix())
	filePath := filepath.Join(h.uploadsDir, fileName)

	dst, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	// Copy with size limit
	_, err = io.CopyN(dst, resp.Body, h.maxFileSize)
	if err != nil && err != io.EOF {
		return "", err
	}

	return filePath, nil
}

// isValidResourceFormat validates resource type and format combinations
func (h *Handler) isValidResourceFormat(resourceType, format string) bool {
	validCombinations := map[string][]string{
		"users":    {"csv", "ndjson", "json"},
		"articles": {"ndjson", "json"},
		"comments": {"ndjson", "json"},
	}

	formats, exists := validCombinations[resourceType]
	if !exists {
		return false
	}

	for _, validFormat := range formats {
		if format == validFormat {
			return true
		}
	}

	return false
}

// Middleware for request logging
func (h *Handler) RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Process request
		c.Next()

		// Log request details
		latency := time.Since(start)
		status := c.Writer.Status()

		fmt.Printf("[%s] %s %s %d %v\n",
			start.Format(time.RFC3339),
			c.Request.Method,
			c.Request.URL.Path,
			status,
			latency,
		)
	}
}

// Middleware for CORS
func (h *Handler) CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, Idempotency-Key")
		c.Header("Access-Control-Expose-Headers", "Content-Length")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// Middleware for rate limiting (simple in-memory implementation)
func (h *Handler) RateLimit() gin.HandlerFunc {
	// This is a simple implementation. In production, use a proper rate limiting library
	requests := make(map[string][]time.Time)

	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		now := time.Now()

		// Clean old requests (older than 1 minute)
		if times, exists := requests[clientIP]; exists {
			var recent []time.Time
			for _, t := range times {
				if now.Sub(t) < time.Minute {
					recent = append(recent, t)
				}
			}
			requests[clientIP] = recent
		}

		// Check rate limit (max 100 requests per minute)
		if len(requests[clientIP]) >= 100 {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Rate limit exceeded",
				"retry_after": 60,
			})
			c.Abort()
			return
		}

		// Add current request
		requests[clientIP] = append(requests[clientIP], now)
		c.Next()
	}
}

// Middleware for request size limiting
func (h *Handler) RequestSizeLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > h.maxFileSize {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":    "Request body too large",
				"max_size": h.maxFileSize,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
