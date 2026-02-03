package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUserValidation(t *testing.T) {
	user := User{
		Email:  "test@example.com",
		Name:   "Test User",
		Role:   "admin",
		Active: true,
	}

	// Test ID generation
	user.GenerateID()
	if user.ID == "" {
		t.Error("Expected ID to be generated")
	}

	// Test UUID format
	_, err := uuid.Parse(user.ID)
	if err != nil {
		t.Errorf("Expected valid UUID, got: %s", user.ID)
	}

	// Test natural key
	if user.GetNaturalKey() != "test@example.com" {
		t.Errorf("Expected natural key to be email, got: %s", user.GetNaturalKey())
	}

	// Test timestamps
	user.SetTimestamps()
	if user.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
	if user.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be set")
	}
}

func TestArticleValidation(t *testing.T) {
	article := Article{
		Slug:     "test-article",
		Title:    "Test Article",
		Body:     "This is a test article body",
		AuthorID: uuid.New().String(),
		Status:   "draft",
		Tags:     []string{"test", "example"},
	}

	// Test ID generation
	article.GenerateID()
	if article.ID == "" {
		t.Error("Expected ID to be generated")
	}

	// Test natural key
	if article.GetNaturalKey() != "test-article" {
		t.Errorf("Expected natural key to be slug, got: %s", article.GetNaturalKey())
	}

	// Test timestamps
	article.SetTimestamps()
	if article.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
	if article.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be set")
	}
}

func TestCommentValidation(t *testing.T) {
	comment := Comment{
		ArticleID: uuid.New().String(),
		UserID:    uuid.New().String(),
		Body:      "This is a test comment",
	}

	// Test ID generation
	comment.GenerateID()
	if comment.ID == "" {
		t.Error("Expected ID to be generated")
	}

	// Test timestamp
	comment.SetTimestamps()
	if comment.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
}

func TestValidationError(t *testing.T) {
	err := ValidationError{
		Row:     1,
		Field:   "email",
		Value:   "invalid-email",
		Message: "Invalid email format",
	}

	if err.Row != 1 {
		t.Errorf("Expected row 1, got %d", err.Row)
	}
	if err.Field != "email" {
		t.Errorf("Expected field 'email', got %s", err.Field)
	}
	if err.Message != "Invalid email format" {
		t.Errorf("Expected specific message, got %s", err.Message)
	}
}

func TestImportJob(t *testing.T) {
	job := ImportJob{
		ID:           uuid.New().String(),
		Status:       "pending",
		ResourceType: "users",
		FileName:     "users.csv",
		CreatedAt:    time.Now(),
		Errors:       []ValidationError{},
	}

	if job.Status != "pending" {
		t.Errorf("Expected status 'pending', got %s", job.Status)
	}
	if job.ResourceType != "users" {
		t.Errorf("Expected resource type 'users', got %s", job.ResourceType)
	}
	if len(job.Errors) != 0 {
		t.Errorf("Expected empty errors slice, got %d errors", len(job.Errors))
	}
}

func TestExportJob(t *testing.T) {
	filters := map[string]string{
		"role":   "admin",
		"active": "true",
	}

	job := ExportJob{
		ID:           uuid.New().String(),
		Status:       "pending",
		ResourceType: "users",
		Format:       "csv",
		Filters:      filters,
		CreatedAt:    time.Now(),
	}

	if job.Format != "csv" {
		t.Errorf("Expected format 'csv', got %s", job.Format)
	}
	if len(job.Filters) != 2 {
		t.Errorf("Expected 2 filters, got %d", len(job.Filters))
	}
	if job.Filters["role"] != "admin" {
		t.Errorf("Expected role filter 'admin', got %s", job.Filters["role"])
	}
}
