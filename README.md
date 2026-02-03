# Bulk Import/Export API

A Go-based REST API for bulk data import and export operations, designed to handle large datasets (up to 1M records) with streaming processing and async job management.

## Features

- **Bulk Import**: Asynchronous import of users (CSV), articles (NDJSON), and comments (NDJSON)
- **Bulk Export**: Streaming and async export with multiple formats (CSV, NDJSON, JSON)
- **Validation**: Per-record validation with error collection and foreign key checking
- **Streaming**: O(1) memory usage for large datasets
- **Job Management**: Async job tracking with status and progress reporting
- **Idempotency**: Duplicate request prevention using Idempotency-Key header
- **Rate Limiting**: Built-in request rate limiting
- **Monitoring**: Prometheus metrics endpoint

## Architecture

### Data Models
- **Users**: `id, email, name, role, active, created_at, updated_at`
- **Articles**: `id, slug, title, body, author_id, tags, published_at, status`
- **Comments**: `id, article_id, user_id, body, created_at`

### Key Components
- **Storage Layer**: PostgreSQL with batch operations and upsert logic
- **Validation Layer**: Per-record validation with error collection
- **Job Management**: Async processing with status tracking
- **Streaming Processor**: Memory-efficient data processing
- **HTTP Handlers**: RESTful API with multipart upload support

## Quick Start

### Prerequisites
- Go 1.21+
- PostgreSQL 12+
- 100MB+ available disk space for uploads/exports

### Installation

1. Clone the repository:
```bash
git clone <repository-url>
cd bulk-import-export-api
```

2. Install dependencies:
```bash
go mod tidy
```

3. Set up PostgreSQL database:
```bash
createdb bulk_api
```

4. Configure environment variables:
```bash
export DATABASE_URL="postgres://user:password@localhost/bulk_api?sslmode=disable"
export SERVER_ADDRESS=":8080"
export UPLOADS_DIR="./uploads"
export EXPORTS_DIR="./exports"
```

5. Run the server:
```bash
go run cmd/server/main.go
```

## API Endpoints

### Import Jobs (Async)

#### Create Import Job
```bash
# Multipart file upload
curl -X POST http://localhost:8080/v1/imports \
  -H "Idempotency-Key: unique-key-123" \
  -F "file=@users.csv" \
  -F "resource_type=users" \
  -F "format=csv"

# JSON with remote file URL  
curl -X POST http://localhost:8080/v1/imports \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: unique-key-124" \
  -d '{
    "resource_type": "articles",
    "format": "ndjson",
    "file_url": "https://example.com/articles.ndjson"
  }'
```

#### Check Import Status
```bash
curl http://localhost:8080/v1/imports/{job_id}
```

### Export (Streaming + Async)

#### Streaming Export
```bash
# Stream users as CSV
curl "http://localhost:8080/v1/exports?resource=users&format=csv&role=admin" > users.csv

# Stream articles as NDJSON
curl "http://localhost:8080/v1/exports?resource=articles&format=ndjson&status=published" > articles.ndjson
```

#### Async Export Job
```bash
curl -X POST http://localhost:8080/v1/exports \
  -H "Content-Type: application/json" \
  -d '{
    "resource_type": "comments",
    "format": "ndjson",
    "filters": {"article_id": "some-uuid"}
  }'
```

#### Check Export Status
```bash
curl http://localhost:8080/v1/exports/{job_id}
```

### Admin Endpoints

```bash
# Health check
curl http://localhost:8080/health

# Job statistics
curl http://localhost:8080/v1/admin/stats

# Prometheus metrics
curl http://localhost:8080/metrics
```

## Data Formats

### Users (CSV)
```csv
id,email,name,role,active,created_at,updated_at
uuid,user@example.com,John Doe,admin,true,2024-01-01T00:00:00Z,2024-01-01T00:00:00Z
```

### Articles (NDJSON)
```jsonl
{"id":"uuid","slug":"my-article","title":"Article Title","body":"Content...","author_id":"uuid","tags":["tag1"],"status":"published","published_at":"2024-01-01T00:00:00Z"}
```

### Comments (NDJSON)
```jsonl
{"id":"uuid","article_id":"uuid","user_id":"uuid","body":"Comment text","created_at":"2024-01-01T00:00:00Z"}
```

## Validation Rules

### Users
- Email: Valid format and unique
- Role: Must be `admin`, `manager`, or `reader`
- Active: Boolean value

### Articles
- Slug: Kebab-case format and unique
- Author ID: Must reference existing user
- Status: `draft` or `published`
- Business rule: Draft articles cannot have `published_at`

### Comments
- Article ID: Must reference existing article
- User ID: Must reference existing user
- Body: ≤ 500 words, cannot be empty

## Performance

- **Throughput**: 5k rows/sec for NDJSON export
- **Memory**: O(1) streaming processing
- **Batch Size**: 1000 records per database batch
- **File Size Limit**: 100MB per upload
- **Rate Limit**: 100 requests per minute per IP

## Testing

### Test Data
The repository includes test datasets in `import_testdata_all_in_one/`:
- `users_huge.csv` (10K rows) - includes validation test cases
- `articles_huge.ndjson` (15K lines) - includes foreign key violations
- `comments_huge.ndjson` (20K lines) - includes body length violations

### Running Tests
```bash
# Run unit tests
go test ./...

# Test with sample data
curl -X POST http://localhost:8080/v1/imports \
  -F "file=@import_testdata_all_in_one/users_huge.csv" \
  -F "resource_type=users" \
  -F "format=csv"
```

## Monitoring

### Prometheus Metrics
The API exposes metrics at `/metrics` endpoint:
- Request duration
- Request count by status code
- Job processing metrics
- Database connection pool stats

### Logging
Structured logging with:
- Request/response logging
- Job processing events
- Error tracking with context
- Performance metrics

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `SERVER_ADDRESS` | `:8080` | Server listen address |
| `DATABASE_URL` | `postgres://...` | PostgreSQL connection string |
| `UPLOADS_DIR` | `./uploads` | Directory for uploaded files |
| `EXPORTS_DIR` | `./exports` | Directory for export files |
| `GIN_MODE` | `release` | Gin framework mode |

## Development

### Project Structure
```
cmd/server/           # Main application entry point
internal/
  handlers/          # HTTP request handlers
  models/            # Data structures and models
  storage/           # Database layer
  validation/        # Record validation logic
pkg/
  jobs/              # Async job management
  streaming/         # Streaming data processor
```

### Key Design Patterns
- **Streaming Processing**: Never load full datasets into memory
- **Batch Operations**: Process records in configurable batches
- **Error Collection**: Continue processing on validation errors
- **Async Jobs**: Long-running imports as background jobs
- **Idempotency**: Prevent duplicate processing

## Docker Support

```dockerfile
# Build stage
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod tidy && go build -o bulk-api cmd/server/main.go

# Runtime stage
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /root/
COPY --from=builder /app/bulk-api .
EXPOSE 8080
CMD ["./bulk-api"]
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass: `go test ./...`
5. Submit a pull request

## License

This project is licensed under the MIT License.
