package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user in the system
type User struct {
	ID        string    `json:"id" csv:"id" validate:"omitempty,uuid"`
	Email     string    `json:"email" csv:"email" validate:"required,email"`
	Name      string    `json:"name" csv:"name" validate:"required"`
	Role      string    `json:"role" csv:"role" validate:"required,oneof=admin manager reader"`
	Active    bool      `json:"active" csv:"active"`
	CreatedAt time.Time `json:"created_at" csv:"created_at"`
	UpdatedAt time.Time `json:"updated_at" csv:"updated_at"`
}

// Article represents an article in the system
type Article struct {
	ID          string     `json:"id" validate:"omitempty,uuid"`
	Slug        string     `json:"slug" validate:"required"`
	Title       string     `json:"title" validate:"required"`
	Body        string     `json:"body" validate:"required"`
	AuthorID    string     `json:"author_id" validate:"required,uuid"`
	Tags        []string   `json:"tags"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Status      string     `json:"status" validate:"required,oneof=draft published"`
	CreatedAt   time.Time  `json:"created_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at,omitempty"`
}

// Comment represents a comment in the system
type Comment struct {
	ID        string    `json:"id" validate:"omitempty,uuid"`
	ArticleID string    `json:"article_id" validate:"required,uuid"`
	UserID    string    `json:"user_id" validate:"required,uuid"`
	Body      string    `json:"body" validate:"required"`
	CreatedAt time.Time `json:"created_at"`
}

// ValidationError represents a validation error for a specific record
type ValidationError struct {
	Row     int                    `json:"row"`
	Field   string                 `json:"field"`
	Value   interface{}            `json:"value"`
	Message string                 `json:"message"`
	Record  map[string]interface{} `json:"record,omitempty"`
}

// ImportJob represents an asynchronous import job
type ImportJob struct {
	ID           string            `json:"id"`
	Status       string            `json:"status"` // pending, processing, completed, failed
	ResourceType string            `json:"resource_type"`
	FileName     string            `json:"file_name"`
	TotalRecords int               `json:"total_records"`
	ValidRecords int               `json:"valid_records"`
	ErrorRecords int               `json:"error_records"`
	Errors       []ValidationError `json:"errors"`
	CreatedAt    time.Time         `json:"created_at"`
	CompletedAt  *time.Time        `json:"completed_at,omitempty"`
	Progress     int               `json:"progress"` // percentage
}

// ExportJob represents an asynchronous export job
type ExportJob struct {
	ID           string            `json:"id"`
	Status       string            `json:"status"` // pending, processing, completed, failed
	ResourceType string            `json:"resource_type"`
	Format       string            `json:"format"`
	Filters      map[string]string `json:"filters"`
	TotalRecords int               `json:"total_records"`
	DownloadURL  string            `json:"download_url,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	CompletedAt  *time.Time        `json:"completed_at,omitempty"`
	Progress     int               `json:"progress"` // percentage
}

// ImportRequest represents a request to import data
type ImportRequest struct {
	ResourceType string `json:"resource_type" validate:"required,oneof=users articles comments"`
	FileURL      string `json:"file_url,omitempty"`
	Format       string `json:"format" validate:"required,oneof=csv ndjson"`
}

// ExportRequest represents a request to export data
type ExportRequest struct {
	ResourceType string            `json:"resource_type" validate:"required,oneof=users articles comments"`
	Format       string            `json:"format" validate:"required,oneof=csv ndjson json"`
	Filters      map[string]string `json:"filters,omitempty"`
	Fields       []string          `json:"fields,omitempty"`
}

// GetNaturalKey returns the natural key for upsert operations
func (u *User) GetNaturalKey() string {
	return u.Email
}

// GetNaturalKey returns the natural key for upsert operations
func (a *Article) GetNaturalKey() string {
	return a.Slug
}

// GenerateID generates a new UUID for the record if not set
func (u *User) GenerateID() {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
}

// GenerateID generates a new UUID for the record if not set
func (a *Article) GenerateID() {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
}

// GenerateID generates a new UUID for the record if not set
func (c *Comment) GenerateID() {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
}

// SetTimestamps sets created_at and updated_at for new records
func (u *User) SetTimestamps() {
	now := time.Now()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now
}

// SetTimestamps sets created_at and updated_at for new records
func (a *Article) SetTimestamps() {
	now := time.Now()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
}

// SetTimestamps sets created_at for new comments
func (c *Comment) SetTimestamps() {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
}
