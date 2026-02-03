package storage

import (
	"database/sql"
	"fmt"
	"strings"

	"bulk-import-export-api/internal/models"

	"github.com/lib/pq"
)

// Storage provides database operations
type Storage struct {
	db *sql.DB
}

// NewStorage creates a new storage instance
func NewStorage(db *sql.DB) *Storage {
	return &Storage{db: db}
}

// InitSchema creates the database tables
func (s *Storage) InitSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY,
			email VARCHAR(255) UNIQUE NOT NULL,
			name VARCHAR(255) NOT NULL,
			role VARCHAR(50) NOT NULL CHECK (role IN ('admin', 'manager', 'reader')),
			active BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS articles (
			id UUID PRIMARY KEY,
			slug VARCHAR(255) UNIQUE NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			author_id UUID NOT NULL REFERENCES users(id),
			tags TEXT[],
			published_at TIMESTAMP,
			status VARCHAR(20) NOT NULL CHECK (status IN ('draft', 'published')),
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS comments (
			id UUID PRIMARY KEY,
			article_id UUID NOT NULL REFERENCES articles(id),
			user_id UUID NOT NULL REFERENCES users(id),
			body TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);

		-- Indexes for better performance
		CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
		CREATE INDEX IF NOT EXISTS idx_articles_slug ON articles(slug);
		CREATE INDEX IF NOT EXISTS idx_articles_author ON articles(author_id);
		CREATE INDEX IF NOT EXISTS idx_comments_article ON comments(article_id);
		CREATE INDEX IF NOT EXISTS idx_comments_user ON comments(user_id);
	`

	_, err := s.db.Exec(schema)
	return err
}

// UserExists checks if a user with the given ID exists
func (s *Storage) UserExists(id string) bool {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)"
	s.db.QueryRow(query, id).Scan(&exists)
	return exists
}

// ArticleExists checks if an article with the given ID exists
func (s *Storage) ArticleExists(id string) bool {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM articles WHERE id = $1)"
	s.db.QueryRow(query, id).Scan(&exists)
	return exists
}

// EmailExists checks if an email already exists
func (s *Storage) EmailExists(email string) bool {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)"
	s.db.QueryRow(query, email).Scan(&exists)
	return exists
}

// SlugExists checks if a slug already exists
func (s *Storage) SlugExists(slug string) bool {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM articles WHERE slug = $1)"
	s.db.QueryRow(query, slug).Scan(&exists)
	return exists
}

// BatchInsertUsers inserts multiple users in a single transaction
func (s *Storage) BatchInsertUsers(users []models.User) error {
	if len(users) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO users (id, email, name, role, active, created_at, updated_at) 
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (email) DO UPDATE SET
			name = EXCLUDED.name,
			role = EXCLUDED.role,
			active = EXCLUDED.active,
			updated_at = EXCLUDED.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, user := range users {
		_, err = stmt.Exec(user.ID, user.Email, user.Name, user.Role, user.Active,
			user.CreatedAt, user.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// BatchInsertArticles inserts multiple articles in a single transaction
func (s *Storage) BatchInsertArticles(articles []models.Article) error {
	if len(articles) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO articles (id, slug, title, body, author_id, tags, published_at, status, created_at, updated_at) 
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (slug) DO UPDATE SET
			title = EXCLUDED.title,
			body = EXCLUDED.body,
			author_id = EXCLUDED.author_id,
			tags = EXCLUDED.tags,
			published_at = EXCLUDED.published_at,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, article := range articles {
		_, err = stmt.Exec(article.ID, article.Slug, article.Title, article.Body,
			article.AuthorID, pq.Array(article.Tags), article.PublishedAt, article.Status,
			article.CreatedAt, article.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// BatchInsertComments inserts multiple comments in a single transaction
func (s *Storage) BatchInsertComments(comments []models.Comment) error {
	if len(comments) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO comments (id, article_id, user_id, body, created_at) 
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			article_id = EXCLUDED.article_id,
			user_id = EXCLUDED.user_id,
			body = EXCLUDED.body,
			created_at = EXCLUDED.created_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, comment := range comments {
		_, err = stmt.Exec(comment.ID, comment.ArticleID, comment.UserID, comment.Body, comment.CreatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetUsers retrieves users with optional filters for export
func (s *Storage) GetUsers(filters map[string]string) (*sql.Rows, error) {
	query := "SELECT id, email, name, role, active, created_at, updated_at FROM users"
	where := []string{}
	args := []interface{}{}
	argCount := 0

	// Apply filters
	if role, ok := filters["role"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("role = $%d", argCount))
		args = append(args, role)
	}
	if active, ok := filters["active"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("active = $%d", argCount))
		args = append(args, active == "true")
	}

	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	query += " ORDER BY created_at"

	return s.db.Query(query, args...)
}

// GetArticles retrieves articles with optional filters for export
func (s *Storage) GetArticles(filters map[string]string) (*sql.Rows, error) {
	query := `
		SELECT a.id, a.slug, a.title, a.body, a.author_id, a.tags, 
			   a.published_at, a.status, a.created_at, a.updated_at
		FROM articles a
	`
	where := []string{}
	args := []interface{}{}
	argCount := 0

	// Apply filters
	if status, ok := filters["status"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("a.status = $%d", argCount))
		args = append(args, status)
	}
	if authorID, ok := filters["author_id"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("a.author_id = $%d", argCount))
		args = append(args, authorID)
	}

	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	query += " ORDER BY a.created_at"

	return s.db.Query(query, args...)
}

// GetComments retrieves comments with optional filters for export
func (s *Storage) GetComments(filters map[string]string) (*sql.Rows, error) {
	query := "SELECT id, article_id, user_id, body, created_at FROM comments"
	where := []string{}
	args := []interface{}{}
	argCount := 0

	// Apply filters
	if articleID, ok := filters["article_id"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("article_id = $%d", argCount))
		args = append(args, articleID)
	}
	if userID, ok := filters["user_id"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("user_id = $%d", argCount))
		args = append(args, userID)
	}

	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	query += " ORDER BY created_at"

	return s.db.Query(query, args...)
}

// CountUsers returns the total number of users matching filters
func (s *Storage) CountUsers(filters map[string]string) (int, error) {
	query := "SELECT COUNT(*) FROM users"
	where := []string{}
	args := []interface{}{}
	argCount := 0

	if role, ok := filters["role"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("role = $%d", argCount))
		args = append(args, role)
	}
	if active, ok := filters["active"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("active = $%d", argCount))
		args = append(args, active == "true")
	}

	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var count int
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// CountArticles returns the total number of articles matching filters
func (s *Storage) CountArticles(filters map[string]string) (int, error) {
	query := "SELECT COUNT(*) FROM articles"
	where := []string{}
	args := []interface{}{}
	argCount := 0

	if status, ok := filters["status"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("status = $%d", argCount))
		args = append(args, status)
	}
	if authorID, ok := filters["author_id"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("author_id = $%d", argCount))
		args = append(args, authorID)
	}

	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var count int
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// CountComments returns the total number of comments matching filters
func (s *Storage) CountComments(filters map[string]string) (int, error) {
	query := "SELECT COUNT(*) FROM comments"
	where := []string{}
	args := []interface{}{}
	argCount := 0

	if articleID, ok := filters["article_id"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("article_id = $%d", argCount))
		args = append(args, articleID)
	}
	if userID, ok := filters["user_id"]; ok {
		argCount++
		where = append(where, fmt.Sprintf("user_id = $%d", argCount))
		args = append(args, userID)
	}

	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var count int
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}
