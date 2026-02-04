package streaming

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/vairarchi/bulk-import-export-api/internal/models"
	"github.com/vairarchi/bulk-import-export-api/internal/validation"
	"github.com/vairarchi/bulk-import-export-api/pkg/jobs"
)

const (
	BatchSize = 1000 // Process records in batches of 1000
)

// Processor handles streaming import/export operations
type Processor struct {
	storage    Storage
	jobManager *jobs.JobManager
	exportDir  string // Directory to store export files
}

// Storage interface for streaming operations
type Storage interface {
	BatchInsertUsers(users []models.User) error
	BatchInsertArticles(articles []models.Article) error
	BatchInsertComments(comments []models.Comment) error
	GetUsers(filters map[string]string) (*sql.Rows, error)
	GetArticles(filters map[string]string) (*sql.Rows, error)
	GetComments(filters map[string]string) (*sql.Rows, error)
	UserExists(id string) bool
	ArticleExists(id string) bool
	EmailExists(email string) bool
	SlugExists(slug string) bool
}

// NewProcessor creates a new streaming processor
func NewProcessor(storage Storage, jobManager *jobs.JobManager, exportDir string) *Processor {
	return &Processor{
		storage:    storage,
		jobManager: jobManager,
		exportDir:  exportDir,
	}
}

// ProcessImport processes import data with streaming and batching
func (p *Processor) ProcessImport(ctx context.Context, jobID string, resourceType string, filePath string, format string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	switch resourceType {
	case "users":
		if format == "csv" {
			return p.processUsersCSV(ctx, jobID, file)
		}
		return fmt.Errorf("unsupported format for users: %s", format)
	case "articles":
		if format == "ndjson" {
			return p.processArticlesNDJSON(ctx, jobID, file)
		}
		return fmt.Errorf("unsupported format for articles: %s", format)
	case "comments":
		if format == "ndjson" {
			return p.processCommentsNDJSON(ctx, jobID, file)
		}
		return fmt.Errorf("unsupported format for comments: %s", format)
	default:
		return fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

// processUsersCSV processes users from CSV format with streaming
func (p *Processor) processUsersCSV(ctx context.Context, jobID string, reader io.Reader) error {
	csvReader := csv.NewReader(reader)
	validator := validation.NewBatchValidator(p.storage)

	// Read header
	header, err := csvReader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV header: %w", err)
	}

	// Find column indices
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	totalProcessed := 0
	totalValid := 0
	batch := make([]models.User, 0, BatchSize)
	rowNumber := 1 // Start after header

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Handle CSV parsing error - we'll track this and update job directly
			parsingError := models.ValidationError{
				Row:     rowNumber + 1,
				Field:   "csv",
				Message: fmt.Sprintf("CSV parsing error: %v", err),
			}
			p.jobManager.UpdateImportJob(jobID, "processing", 0, totalProcessed, totalValid, 1, []models.ValidationError{parsingError})
			rowNumber++
			continue
		}

		user, parseErr := p.parseUserFromCSV(record, colIndex)
		if parseErr != nil {
			// Add parsing error to validator
			validationErrors := []models.ValidationError{{
				Row:     rowNumber + 1,
				Field:   "parsing",
				Message: parseErr.Error(),
			}}
			// We'll track parsing errors separately and combine them later
			p.jobManager.UpdateImportJob(jobID, "processing", 0, totalProcessed, totalValid, 1, validationErrors)
		} else {
			batch = append(batch, user)
		}

		rowNumber++
		totalProcessed++

		// Process batch when full
		if len(batch) >= BatchSize {
			validUsers := validator.ValidateUsers(batch, totalProcessed-len(batch))
			if len(validUsers) > 0 {
				if err := p.storage.BatchInsertUsers(validUsers); err != nil {
					return fmt.Errorf("failed to insert user batch: %w", err)
				}
				totalValid += len(validUsers)
			}

			// Update job progress
			progress := (totalProcessed * 50) / (totalProcessed + 1000) // Rough progress estimate
			p.jobManager.UpdateImportJob(jobID, "processing", progress, totalProcessed, totalValid,
				len(validator.GetErrors()), validator.GetErrors())

			batch = make([]models.User, 0, BatchSize)
		}
	}

	// Process remaining batch
	if len(batch) > 0 {
		validUsers := validator.ValidateUsers(batch, totalProcessed-len(batch))
		if len(validUsers) > 0 {
			if err := p.storage.BatchInsertUsers(validUsers); err != nil {
				return fmt.Errorf("failed to insert final user batch: %w", err)
			}
			totalValid += len(validUsers)
		}
	}

	// Mark job as completed
	allErrors := validator.GetErrors()
	status := "completed"
	if len(allErrors) > 0 && totalValid == 0 {
		status = "failed"
	}

	p.jobManager.UpdateImportJob(jobID, status, 100, totalProcessed, totalValid,
		len(allErrors), allErrors)

	return nil
}

// processArticlesNDJSON processes articles from NDJSON format
func (p *Processor) processArticlesNDJSON(ctx context.Context, jobID string, reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	validator := validation.NewBatchValidator(p.storage)

	totalProcessed := 0
	totalValid := 0
	batch := make([]models.Article, 0, BatchSize)
	rowNumber := 0

	for decoder.More() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var article models.Article
		if err := decoder.Decode(&article); err != nil {
			// Handle JSON parsing error - we'll track this and update job directly
			parsingError := models.ValidationError{
				Row:     rowNumber + 1,
				Field:   "json",
				Message: fmt.Sprintf("JSON parsing error: %v", err),
			}
			p.jobManager.UpdateImportJob(jobID, "processing", 0, totalProcessed, totalValid, 1, []models.ValidationError{parsingError})
		} else {
			batch = append(batch, article)
		}

		rowNumber++
		totalProcessed++

		// Process batch when full
		if len(batch) >= BatchSize {
			validArticles := validator.ValidateArticles(batch, totalProcessed-len(batch))
			if len(validArticles) > 0 {
				if err := p.storage.BatchInsertArticles(validArticles); err != nil {
					return fmt.Errorf("failed to insert article batch: %w", err)
				}
				totalValid += len(validArticles)
			}

			// Update job progress
			progress := (totalProcessed * 50) / (totalProcessed + 1000) // Rough progress estimate
			p.jobManager.UpdateImportJob(jobID, "processing", progress, totalProcessed, totalValid,
				len(validator.GetErrors()), validator.GetErrors())

			batch = make([]models.Article, 0, BatchSize)
		}
	}

	// Process remaining batch
	if len(batch) > 0 {
		validArticles := validator.ValidateArticles(batch, totalProcessed-len(batch))
		if len(validArticles) > 0 {
			if err := p.storage.BatchInsertArticles(validArticles); err != nil {
				return fmt.Errorf("failed to insert final article batch: %w", err)
			}
			totalValid += len(validArticles)
		}
	}

	// Mark job as completed
	allErrors := validator.GetErrors()
	status := "completed"
	if len(allErrors) > 0 && totalValid == 0 {
		status = "failed"
	}

	p.jobManager.UpdateImportJob(jobID, status, 100, totalProcessed, totalValid,
		len(allErrors), allErrors)

	return nil
}

// processCommentsNDJSON processes comments from NDJSON format
func (p *Processor) processCommentsNDJSON(ctx context.Context, jobID string, reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	validator := validation.NewBatchValidator(p.storage)

	totalProcessed := 0
	totalValid := 0
	batch := make([]models.Comment, 0, BatchSize)
	rowNumber := 0

	for decoder.More() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var comment models.Comment
		if err := decoder.Decode(&comment); err != nil {
			// Handle JSON parsing error - we'll track this and update job directly
			parsingError := models.ValidationError{
				Row:     rowNumber + 1,
				Field:   "json",
				Message: fmt.Sprintf("JSON parsing error: %v", err),
			}
			p.jobManager.UpdateImportJob(jobID, "processing", 0, totalProcessed, totalValid, 1, []models.ValidationError{parsingError})
		} else {
			batch = append(batch, comment)
		}

		rowNumber++
		totalProcessed++

		// Process batch when full
		if len(batch) >= BatchSize {
			validComments := validator.ValidateComments(batch, totalProcessed-len(batch))
			if len(validComments) > 0 {
				if err := p.storage.BatchInsertComments(validComments); err != nil {
					return fmt.Errorf("failed to insert comment batch: %w", err)
				}
				totalValid += len(validComments)
			}

			// Update job progress
			progress := (totalProcessed * 50) / (totalProcessed + 1000) // Rough progress estimate
			p.jobManager.UpdateImportJob(jobID, "processing", progress, totalProcessed, totalValid,
				len(validator.GetErrors()), validator.GetErrors())

			batch = make([]models.Comment, 0, BatchSize)
		}
	}

	// Process remaining batch
	if len(batch) > 0 {
		validComments := validator.ValidateComments(batch, totalProcessed-len(batch))
		if len(validComments) > 0 {
			if err := p.storage.BatchInsertComments(validComments); err != nil {
				return fmt.Errorf("failed to insert final comment batch: %w", err)
			}
			totalValid += len(validComments)
		}
	}

	// Mark job as completed
	allErrors := validator.GetErrors()
	status := "completed"
	if len(allErrors) > 0 && totalValid == 0 {
		status = "failed"
	}

	p.jobManager.UpdateImportJob(jobID, status, 100, totalProcessed, totalValid,
		len(allErrors), allErrors)

	return nil
}

// parseUserFromCSV parses a user from CSV record
func (p *Processor) parseUserFromCSV(record []string, colIndex map[string]int) (models.User, error) {
	user := models.User{}

	if idx, ok := colIndex["id"]; ok && idx < len(record) {
		user.ID = strings.TrimSpace(record[idx])
	}
	if idx, ok := colIndex["email"]; ok && idx < len(record) {
		user.Email = strings.TrimSpace(record[idx])
	}
	if idx, ok := colIndex["name"]; ok && idx < len(record) {
		user.Name = strings.TrimSpace(record[idx])
	}
	if idx, ok := colIndex["role"]; ok && idx < len(record) {
		user.Role = strings.TrimSpace(record[idx])
	}
	if idx, ok := colIndex["active"]; ok && idx < len(record) {
		active, err := strconv.ParseBool(strings.TrimSpace(record[idx]))
		if err != nil {
			return user, fmt.Errorf("invalid active value: %s", record[idx])
		}
		user.Active = active
	}
	if idx, ok := colIndex["created_at"]; ok && idx < len(record) {
		createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(record[idx]))
		if err != nil {
			return user, fmt.Errorf("invalid created_at value: %s", record[idx])
		}
		user.CreatedAt = createdAt
	}
	if idx, ok := colIndex["updated_at"]; ok && idx < len(record) {
		updatedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(record[idx]))
		if err != nil {
			return user, fmt.Errorf("invalid updated_at value: %s", record[idx])
		}
		user.UpdatedAt = updatedAt
	}

	return user, nil
}

// ProcessExport processes export requests and returns the download URL
func (p *Processor) ProcessExport(ctx context.Context, jobID string, resourceType string, format string, filters map[string]string) (string, error) {
	fileName := fmt.Sprintf("%s_%s_%d.%s", resourceType, format, time.Now().Unix(), format)
	filePath := filepath.Join(p.exportDir, fileName)

	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create export file: %w", err)
	}
	defer file.Close()

	switch resourceType {
	case "users":
		err = p.exportUsers(ctx, jobID, file, format, filters)
	case "articles":
		err = p.exportArticles(ctx, jobID, file, format, filters)
	case "comments":
		err = p.exportComments(ctx, jobID, file, format, filters)
	default:
		return "", fmt.Errorf("unsupported resource type: %s", resourceType)
	}

	if err != nil {
		os.Remove(filePath)
		return "", err
	}

	// Return relative path as download URL
	return fmt.Sprintf("/downloads/%s", fileName), nil
}

// exportUsers exports users to the specified format
func (p *Processor) exportUsers(ctx context.Context, jobID string, writer io.Writer, format string, filters map[string]string) error {
	rows, err := p.storage.GetUsers(filters)
	if err != nil {
		return fmt.Errorf("failed to get users: %w", err)
	}
	defer rows.Close()

	processed := 0
	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()

	// Write CSV header for CSV format
	if format == "csv" {
		csvWriter.Write([]string{"id", "email", "name", "role", "active", "created_at", "updated_at"})
	}

	for rows.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var user models.User
		err := rows.Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.Active,
			&user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to scan user: %w", err)
		}

		switch format {
		case "csv":
			record := []string{
				user.ID,
				user.Email,
				user.Name,
				user.Role,
				strconv.FormatBool(user.Active),
				user.CreatedAt.Format(time.RFC3339),
				user.UpdatedAt.Format(time.RFC3339),
			}
			csvWriter.Write(record)
		case "ndjson":
			jsonBytes, _ := json.Marshal(user)
			fmt.Fprintln(writer, string(jsonBytes))
		case "json":
			// For JSON format, we'd need to collect all records first
			// This is less memory efficient for large datasets
			jsonBytes, _ := json.Marshal(user)
			fmt.Fprintln(writer, string(jsonBytes))
		}

		processed++
		if processed%BatchSize == 0 {
			progress := min(90, (processed*90)/10000) // Rough progress estimate
			p.jobManager.UpdateExportJob(jobID, "processing", progress, processed, "")
		}
	}

	return rows.Err()
}

// exportArticles exports articles to the specified format
func (p *Processor) exportArticles(ctx context.Context, jobID string, writer io.Writer, format string, filters map[string]string) error {
	rows, err := p.storage.GetArticles(filters)
	if err != nil {
		return fmt.Errorf("failed to get articles: %w", err)
	}
	defer rows.Close()

	processed := 0

	for rows.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var article models.Article
		err := rows.Scan(&article.ID, &article.Slug, &article.Title, &article.Body,
			&article.AuthorID, pq.Array(&article.Tags), &article.PublishedAt, &article.Status,
			&article.CreatedAt, &article.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to scan article: %w", err)
		}

		jsonBytes, _ := json.Marshal(article)
		fmt.Fprintln(writer, string(jsonBytes))

		processed++
		if processed%BatchSize == 0 {
			progress := min(90, (processed*90)/10000) // Rough progress estimate
			p.jobManager.UpdateExportJob(jobID, "processing", progress, processed, "")
		}
	}

	return rows.Err()
}

// exportComments exports comments to the specified format
func (p *Processor) exportComments(ctx context.Context, jobID string, writer io.Writer, format string, filters map[string]string) error {
	rows, err := p.storage.GetComments(filters)
	if err != nil {
		return fmt.Errorf("failed to get comments: %w", err)
	}
	defer rows.Close()

	processed := 0

	for rows.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var comment models.Comment
		err := rows.Scan(&comment.ID, &comment.ArticleID, &comment.UserID, &comment.Body,
			&comment.CreatedAt)
		if err != nil {
			return fmt.Errorf("failed to scan comment: %w", err)
		}

		jsonBytes, _ := json.Marshal(comment)
		fmt.Fprintln(writer, string(jsonBytes))

		processed++
		if processed%BatchSize == 0 {
			progress := min(90, (processed*90)/10000) // Rough progress estimate
			p.jobManager.UpdateExportJob(jobID, "processing", progress, processed, "")
		}
	}

	return rows.Err()
}

// StreamExport streams export data directly to HTTP response
func (p *Processor) StreamExport(w http.ResponseWriter, resourceType string, format string, filters map[string]string) error {
	// Set appropriate headers
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.%s", resourceType, format))

	// Create a flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	switch resourceType {
	case "users":
		return p.streamUsersExport(w, flusher, format, filters)
	case "articles":
		return p.streamArticlesExport(w, flusher, format, filters)
	case "comments":
		return p.streamCommentsExport(w, flusher, format, filters)
	default:
		return fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

// streamUsersExport streams users export
func (p *Processor) streamUsersExport(w http.ResponseWriter, flusher http.Flusher, format string, filters map[string]string) error {
	rows, err := p.storage.GetUsers(filters)
	if err != nil {
		return fmt.Errorf("failed to get users: %w", err)
	}
	defer rows.Close()

	processed := 0
	csvWriter := csv.NewWriter(w)

	// Write CSV header for CSV format
	if format == "csv" {
		csvWriter.Write([]string{"id", "email", "name", "role", "active", "created_at", "updated_at"})
		csvWriter.Flush()
		flusher.Flush()
	}

	for rows.Next() {
		var user models.User
		err := rows.Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.Active,
			&user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to scan user: %w", err)
		}

		switch format {
		case "csv":
			record := []string{
				user.ID,
				user.Email,
				user.Name,
				user.Role,
				strconv.FormatBool(user.Active),
				user.CreatedAt.Format(time.RFC3339),
				user.UpdatedAt.Format(time.RFC3339),
			}
			csvWriter.Write(record)
		case "ndjson":
			jsonBytes, _ := json.Marshal(user)
			fmt.Fprintln(w, string(jsonBytes))
		}

		processed++
		if processed%100 == 0 { // Flush every 100 records
			if format == "csv" {
				csvWriter.Flush()
			}
			flusher.Flush()
		}
	}

	if format == "csv" {
		csvWriter.Flush()
	}
	flusher.Flush()

	return rows.Err()
}

// streamArticlesExport streams articles export
func (p *Processor) streamArticlesExport(w http.ResponseWriter, flusher http.Flusher, format string, filters map[string]string) error {
	rows, err := p.storage.GetArticles(filters)
	if err != nil {
		return fmt.Errorf("failed to get articles: %w", err)
	}
	defer rows.Close()

	processed := 0

	for rows.Next() {
		var article models.Article
		err := rows.Scan(&article.ID, &article.Slug, &article.Title, &article.Body,
			&article.AuthorID, pq.Array(&article.Tags), &article.PublishedAt, &article.Status,
			&article.CreatedAt, &article.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to scan article: %w", err)
		}

		jsonBytes, _ := json.Marshal(article)
		fmt.Fprintln(w, string(jsonBytes))

		processed++
		if processed%100 == 0 { // Flush every 100 records
			flusher.Flush()
		}
	}

	flusher.Flush()
	return rows.Err()
}

// streamCommentsExport streams comments export
func (p *Processor) streamCommentsExport(w http.ResponseWriter, flusher http.Flusher, format string, filters map[string]string) error {
	rows, err := p.storage.GetComments(filters)
	if err != nil {
		return fmt.Errorf("failed to get comments: %w", err)
	}
	defer rows.Close()

	processed := 0

	for rows.Next() {
		var comment models.Comment
		err := rows.Scan(&comment.ID, &comment.ArticleID, &comment.UserID, &comment.Body,
			&comment.CreatedAt)
		if err != nil {
			return fmt.Errorf("failed to scan comment: %w", err)
		}

		jsonBytes, _ := json.Marshal(comment)
		fmt.Fprintln(w, string(jsonBytes))

		processed++
		if processed%100 == 0 { // Flush every 100 records
			flusher.Flush()
		}
	}

	flusher.Flush()
	return rows.Err()
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
