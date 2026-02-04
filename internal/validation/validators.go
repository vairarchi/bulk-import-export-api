package validation

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
	"github.com/vairarchi/bulk-import-export-api/internal/models"
)

var (
	slugRegex = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	validate  = validator.New()
)

// Validator handles validation of records with error collection
type Validator struct {
	storage StorageValidator // Interface for FK validation
}

// StorageValidator interface for foreign key validation
type StorageValidator interface {
	UserExists(id string) bool
	ArticleExists(id string) bool
	EmailExists(email string) bool
	SlugExists(slug string) bool
}

// NewValidator creates a new validator instance
func NewValidator(storage StorageValidator) *Validator {
	return &Validator{storage: storage}
}

// ValidateUser validates a user record and returns validation errors
func (v *Validator) ValidateUser(user *models.User, rowNum int) []models.ValidationError {
	var errors []models.ValidationError

	// Basic struct validation
	if err := validate.Struct(user); err != nil {
		for _, e := range err.(validator.ValidationErrors) {
			errors = append(errors, models.ValidationError{
				Row:     rowNum,
				Field:   strings.ToLower(e.Field()),
				Value:   e.Value(),
				Message: getValidationMessage(e),
			})
		}
	}

	// Custom validations
	if user.Email != "" {
		// Check email uniqueness (skip if doing upsert by email)
		if user.ID != "" && v.storage != nil && v.storage.EmailExists(user.Email) {
			errors = append(errors, models.ValidationError{
				Row:     rowNum,
				Field:   "email",
				Value:   user.Email,
				Message: "email already exists",
			})
		}
	}

	// Role validation
	validRoles := map[string]bool{"admin": true, "manager": true, "reader": true}
	if user.Role != "" && !validRoles[user.Role] {
		errors = append(errors, models.ValidationError{
			Row:     rowNum,
			Field:   "role",
			Value:   user.Role,
			Message: "role must be one of: admin, manager, reader",
		})
	}

	return errors
}

// ValidateArticle validates an article record and returns validation errors
func (v *Validator) ValidateArticle(article *models.Article, rowNum int) []models.ValidationError {
	var errors []models.ValidationError

	// Basic struct validation
	if err := validate.Struct(article); err != nil {
		for _, e := range err.(validator.ValidationErrors) {
			errors = append(errors, models.ValidationError{
				Row:     rowNum,
				Field:   strings.ToLower(e.Field()),
				Value:   e.Value(),
				Message: getValidationMessage(e),
			})
		}
	}

	// Custom validations
	if article.Slug != "" {
		// Validate slug format (kebab-case)
		if !slugRegex.MatchString(article.Slug) {
			errors = append(errors, models.ValidationError{
				Row:     rowNum,
				Field:   "slug",
				Value:   article.Slug,
				Message: "slug must be kebab-case (lowercase letters, numbers, and hyphens only)",
			})
		}

		// Check slug uniqueness (skip if doing upsert by slug)
		if article.ID != "" && v.storage != nil && v.storage.SlugExists(article.Slug) {
			errors = append(errors, models.ValidationError{
				Row:     rowNum,
				Field:   "slug",
				Value:   article.Slug,
				Message: "slug already exists",
			})
		}
	}

	// Author foreign key validation
	if article.AuthorID != "" && v.storage != nil && !v.storage.UserExists(article.AuthorID) {
		errors = append(errors, models.ValidationError{
			Row:     rowNum,
			Field:   "author_id",
			Value:   article.AuthorID,
			Message: "author_id does not exist",
		})
	}

	// Business rule: draft articles cannot have published_at
	if article.Status == "draft" && article.PublishedAt != nil {
		errors = append(errors, models.ValidationError{
			Row:     rowNum,
			Field:   "published_at",
			Value:   article.PublishedAt,
			Message: "draft articles cannot have published_at date",
		})
	}

	// Business rule: published articles should have published_at
	if article.Status == "published" && article.PublishedAt == nil {
		// Auto-set published_at if missing
		now := time.Now()
		article.PublishedAt = &now
	}

	return errors
}

// ValidateComment validates a comment record and returns validation errors
func (v *Validator) ValidateComment(comment *models.Comment, rowNum int) []models.ValidationError {
	var errors []models.ValidationError

	// Basic struct validation
	if err := validate.Struct(comment); err != nil {
		for _, e := range err.(validator.ValidationErrors) {
			errors = append(errors, models.ValidationError{
				Row:     rowNum,
				Field:   strings.ToLower(e.Field()),
				Value:   e.Value(),
				Message: getValidationMessage(e),
			})
		}
	}

	// Custom validations
	// Article foreign key validation
	if comment.ArticleID != "" && v.storage != nil && !v.storage.ArticleExists(comment.ArticleID) {
		errors = append(errors, models.ValidationError{
			Row:     rowNum,
			Field:   "article_id",
			Value:   comment.ArticleID,
			Message: "article_id does not exist",
		})
	}

	// User foreign key validation
	if comment.UserID != "" && v.storage != nil && !v.storage.UserExists(comment.UserID) {
		errors = append(errors, models.ValidationError{
			Row:     rowNum,
			Field:   "user_id",
			Value:   comment.UserID,
			Message: "user_id does not exist",
		})
	}

	// Body length validation (≤ 500 words)
	if comment.Body != "" {
		wordCount := len(strings.Fields(comment.Body))
		if wordCount > 500 {
			errors = append(errors, models.ValidationError{
				Row:     rowNum,
				Field:   "body",
				Value:   fmt.Sprintf("%d words", wordCount),
				Message: "body cannot exceed 500 words",
			})
		}

		// Body cannot be empty
		if strings.TrimSpace(comment.Body) == "" {
			errors = append(errors, models.ValidationError{
				Row:     rowNum,
				Field:   "body",
				Value:   comment.Body,
				Message: "body cannot be empty",
			})
		}

		// Check for extremely long bodies that might cause issues
		if utf8.RuneCountInString(comment.Body) > 10000 {
			errors = append(errors, models.ValidationError{
				Row:     rowNum,
				Field:   "body",
				Value:   fmt.Sprintf("%d characters", utf8.RuneCountInString(comment.Body)),
				Message: "body is too long (over 10,000 characters)",
			})
		}
	}

	return errors
}

// getValidationMessage converts validator errors to user-friendly messages
func getValidationMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", strings.ToLower(e.Field()))
	case "email":
		return "invalid email format"
	case "uuid":
		return "invalid UUID format"
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", strings.ToLower(e.Field()), e.Param())
	default:
		return fmt.Sprintf("%s validation failed: %s", strings.ToLower(e.Field()), e.Tag())
	}
}

// ValidateBatch validates a batch of records and collects all errors
type BatchValidator struct {
	validator *Validator
	errors    []models.ValidationError
}

// NewBatchValidator creates a new batch validator
func NewBatchValidator(storage StorageValidator) *BatchValidator {
	return &BatchValidator{
		validator: NewValidator(storage),
		errors:    make([]models.ValidationError, 0),
	}
}

// ValidateUsers validates a batch of users
func (bv *BatchValidator) ValidateUsers(users []models.User, startRow int) []models.User {
	validUsers := make([]models.User, 0)

	for i, user := range users {
		rowNum := startRow + i + 1
		errors := bv.validator.ValidateUser(&user, rowNum)

		if len(errors) == 0 {
			// Set defaults and generate ID if needed
			user.GenerateID()
			user.SetTimestamps()
			validUsers = append(validUsers, user)
		} else {
			bv.errors = append(bv.errors, errors...)
		}
	}

	return validUsers
}

// ValidateArticles validates a batch of articles
func (bv *BatchValidator) ValidateArticles(articles []models.Article, startRow int) []models.Article {
	validArticles := make([]models.Article, 0)

	for i, article := range articles {
		rowNum := startRow + i + 1
		errors := bv.validator.ValidateArticle(&article, rowNum)

		if len(errors) == 0 {
			// Set defaults and generate ID if needed
			article.GenerateID()
			article.SetTimestamps()
			validArticles = append(validArticles, article)
		} else {
			bv.errors = append(bv.errors, errors...)
		}
	}

	return validArticles
}

// ValidateComments validates a batch of comments
func (bv *BatchValidator) ValidateComments(comments []models.Comment, startRow int) []models.Comment {
	validComments := make([]models.Comment, 0)

	for i, comment := range comments {
		rowNum := startRow + i + 1
		errors := bv.validator.ValidateComment(&comment, rowNum)

		if len(errors) == 0 {
			// Set defaults and generate ID if needed
			comment.GenerateID()
			comment.SetTimestamps()
			validComments = append(validComments, comment)
		} else {
			bv.errors = append(bv.errors, errors...)
		}
	}

	return validComments
}

// GetErrors returns all accumulated validation errors
func (bv *BatchValidator) GetErrors() []models.ValidationError {
	return bv.errors
}

// ClearErrors clears accumulated validation errors
func (bv *BatchValidator) ClearErrors() {
	bv.errors = make([]models.ValidationError, 0)
}
