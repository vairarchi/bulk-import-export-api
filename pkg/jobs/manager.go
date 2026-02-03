package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bulk-import-export-api/internal/models"

	"github.com/google/uuid"
)

// JobManager handles asynchronous job processing
type JobManager struct {
	importJobs map[string]*models.ImportJob
	exportJobs map[string]*models.ExportJob
	mutex      sync.RWMutex
}

// NewJobManager creates a new job manager
func NewJobManager() *JobManager {
	return &JobManager{
		importJobs: make(map[string]*models.ImportJob),
		exportJobs: make(map[string]*models.ExportJob),
	}
}

// CreateImportJob creates a new import job
func (jm *JobManager) CreateImportJob(resourceType, fileName string) *models.ImportJob {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	job := &models.ImportJob{
		ID:           uuid.New().String(),
		Status:       "pending",
		ResourceType: resourceType,
		FileName:     fileName,
		TotalRecords: 0,
		ValidRecords: 0,
		ErrorRecords: 0,
		Errors:       make([]models.ValidationError, 0),
		CreatedAt:    time.Now(),
		Progress:     0,
	}

	jm.importJobs[job.ID] = job
	return job
}

// CreateExportJob creates a new export job
func (jm *JobManager) CreateExportJob(resourceType, format string, filters map[string]string) *models.ExportJob {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	job := &models.ExportJob{
		ID:           uuid.New().String(),
		Status:       "pending",
		ResourceType: resourceType,
		Format:       format,
		Filters:      filters,
		TotalRecords: 0,
		CreatedAt:    time.Now(),
		Progress:     0,
	}

	jm.exportJobs[job.ID] = job
	return job
}

// GetImportJob retrieves an import job by ID
func (jm *JobManager) GetImportJob(id string) (*models.ImportJob, bool) {
	jm.mutex.RLock()
	defer jm.mutex.RUnlock()

	job, exists := jm.importJobs[id]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid concurrent modification
	jobCopy := *job
	jobCopy.Errors = make([]models.ValidationError, len(job.Errors))
	copy(jobCopy.Errors, job.Errors)

	return &jobCopy, true
}

// GetExportJob retrieves an export job by ID
func (jm *JobManager) GetExportJob(id string) (*models.ExportJob, bool) {
	jm.mutex.RLock()
	defer jm.mutex.RUnlock()

	job, exists := jm.exportJobs[id]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid concurrent modification
	jobCopy := *job
	return &jobCopy, true
}

// UpdateImportJob updates the status and progress of an import job
func (jm *JobManager) UpdateImportJob(id string, status string, progress int, totalRecords, validRecords, errorRecords int, errors []models.ValidationError) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	if job, exists := jm.importJobs[id]; exists {
		job.Status = status
		job.Progress = progress
		job.TotalRecords = totalRecords
		job.ValidRecords = validRecords
		job.ErrorRecords = errorRecords

		// Append new errors (limit to prevent memory issues)
		maxErrors := 1000
		if len(job.Errors)+len(errors) > maxErrors {
			// Keep the first 500 and last 500 errors
			if len(job.Errors) < 500 {
				remainingSlots := 500 - len(job.Errors)
				job.Errors = append(job.Errors, errors[:min(remainingSlots, len(errors))]...)
			}
			// Add latest errors, keeping only the most recent 500
			if len(errors) > 500 {
				job.Errors = append(job.Errors[:500], errors[len(errors)-500:]...)
			} else {
				job.Errors = append(job.Errors[:500], errors...)
			}
		} else {
			job.Errors = append(job.Errors, errors...)
		}

		if status == "completed" || status == "failed" {
			now := time.Now()
			job.CompletedAt = &now
		}
	}
}

// UpdateExportJob updates the status and progress of an export job
func (jm *JobManager) UpdateExportJob(id string, status string, progress int, totalRecords int, downloadURL string) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	if job, exists := jm.exportJobs[id]; exists {
		job.Status = status
		job.Progress = progress
		job.TotalRecords = totalRecords
		job.DownloadURL = downloadURL

		if status == "completed" || status == "failed" {
			now := time.Now()
			job.CompletedAt = &now
		}
	}
}

// JobProcessor handles the actual processing of jobs
type JobProcessor struct {
	jobManager *JobManager
	storage    Storage
	processor  DataProcessor
}

// Storage interface for job processing
type Storage interface {
	BatchInsertUsers(users []models.User) error
	BatchInsertArticles(articles []models.Article) error
	BatchInsertComments(comments []models.Comment) error
	CountUsers(filters map[string]string) (int, error)
	CountArticles(filters map[string]string) (int, error)
	CountComments(filters map[string]string) (int, error)
}

// DataProcessor interface for processing import/export data
type DataProcessor interface {
	ProcessImport(ctx context.Context, jobID string, resourceType string, filePath string, format string) error
	ProcessExport(ctx context.Context, jobID string, resourceType string, format string, filters map[string]string) (string, error)
}

// NewJobProcessor creates a new job processor
func NewJobProcessor(jobManager *JobManager, storage Storage, processor DataProcessor) *JobProcessor {
	return &JobProcessor{
		jobManager: jobManager,
		storage:    storage,
		processor:  processor,
	}
}

// ProcessImportJob processes an import job asynchronously
func (jp *JobProcessor) ProcessImportJob(ctx context.Context, jobID string, filePath string, format string) {
	go func() {
		job, exists := jp.jobManager.GetImportJob(jobID)
		if !exists {
			return
		}

		// Mark job as processing
		jp.jobManager.UpdateImportJob(jobID, "processing", 0, 0, 0, 0, nil)

		// Process the import
		err := jp.processor.ProcessImport(ctx, jobID, job.ResourceType, filePath, format)

		if err != nil {
			jp.jobManager.UpdateImportJob(jobID, "failed", 100, 0, 0, 0,
				[]models.ValidationError{{
					Row:     0,
					Field:   "general",
					Message: fmt.Sprintf("Import failed: %v", err),
				}})
		}
	}()
}

// ProcessExportJob processes an export job asynchronously
func (jp *JobProcessor) ProcessExportJob(ctx context.Context, jobID string) {
	go func() {
		job, exists := jp.jobManager.GetExportJob(jobID)
		if !exists {
			return
		}

		// Mark job as processing
		jp.jobManager.UpdateExportJob(jobID, "processing", 0, 0, "")

		// Get total count
		var totalRecords int
		var err error

		switch job.ResourceType {
		case "users":
			totalRecords, err = jp.storage.CountUsers(job.Filters)
		case "articles":
			totalRecords, err = jp.storage.CountArticles(job.Filters)
		case "comments":
			totalRecords, err = jp.storage.CountComments(job.Filters)
		default:
			err = fmt.Errorf("unknown resource type: %s", job.ResourceType)
		}

		if err != nil {
			jp.jobManager.UpdateExportJob(jobID, "failed", 100, 0, "")
			return
		}

		// Process the export
		downloadURL, err := jp.processor.ProcessExport(ctx, jobID, job.ResourceType, job.Format, job.Filters)

		if err != nil {
			jp.jobManager.UpdateExportJob(jobID, "failed", 100, totalRecords, "")
		} else {
			jp.jobManager.UpdateExportJob(jobID, "completed", 100, totalRecords, downloadURL)
		}
	}()
}

// CleanupOldJobs removes jobs older than the specified duration
func (jm *JobManager) CleanupOldJobs(maxAge time.Duration) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	cutoff := time.Now().Add(-maxAge)

	// Clean up import jobs
	for id, job := range jm.importJobs {
		if job.CreatedAt.Before(cutoff) {
			delete(jm.importJobs, id)
		}
	}

	// Clean up export jobs
	for id, job := range jm.exportJobs {
		if job.CreatedAt.Before(cutoff) {
			delete(jm.exportJobs, id)
		}
	}
}

// GetJobStats returns statistics about running jobs
func (jm *JobManager) GetJobStats() map[string]interface{} {
	jm.mutex.RLock()
	defer jm.mutex.RUnlock()

	importStats := make(map[string]int)
	exportStats := make(map[string]int)

	for _, job := range jm.importJobs {
		importStats[job.Status]++
	}

	for _, job := range jm.exportJobs {
		exportStats[job.Status]++
	}

	return map[string]interface{}{
		"import_jobs":       importStats,
		"export_jobs":       exportStats,
		"total_import_jobs": len(jm.importJobs),
		"total_export_jobs": len(jm.exportJobs),
	}
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// IdempotencyManager handles idempotency for import requests
type IdempotencyManager struct {
	keys  map[string]string // idempotency-key -> job-id
	mutex sync.RWMutex
}

// NewIdempotencyManager creates a new idempotency manager
func NewIdempotencyManager() *IdempotencyManager {
	return &IdempotencyManager{
		keys: make(map[string]string),
	}
}

// CheckIdempotency checks if a request with the given key has been processed
func (im *IdempotencyManager) CheckIdempotency(key string) (string, bool) {
	im.mutex.RLock()
	defer im.mutex.RUnlock()

	jobID, exists := im.keys[key]
	return jobID, exists
}

// SetIdempotency stores the mapping of idempotency key to job ID
func (im *IdempotencyManager) SetIdempotency(key, jobID string) {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	im.keys[key] = jobID
}

// CleanupIdempotencyKeys removes old idempotency keys
func (im *IdempotencyManager) CleanupIdempotencyKeys(maxAge time.Duration) {
	// In a real implementation, you'd store timestamps with the keys
	// and clean them up based on age. For simplicity, we'll skip this
	// or implement a simple LRU-based cleanup.
}
