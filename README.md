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
The repository includes comprehensive test datasets in `import_testdata_all_in_one/`:
- `users_huge.csv` (10K rows) - includes invalid emails, duplicate emails, missing IDs, invalid roles
- `articles_huge.ndjson` (15K lines) - includes invalid slugs, FK violations, drafts with published_at
- `comments_huge.ndjson` (20K lines) - includes invalid article/user FKs, overly long bodies, missing fields

These datasets are specifically designed to test validation logic and error handling with a mix of valid and intentionally invalid records.

### Postman Collection
A comprehensive Postman collection is included: `Bulk_Import_Export_API.postman_collection.json`

#### Features:
- **Complete API Coverage** - All import/export endpoints with proper payloads
- **Test Data Integration** - Pre-configured to use the provided test datasets
- **Auto Variable Extraction** - Job IDs automatically captured for request chaining  
- **Built-in Tests** - Response validation, timing checks, and error assertions
- **Smart Workflows** - Import → Status Check → Export → Download sequences

#### Quick Setup:
1. Import `Bulk_Import_Export_API.postman_collection.json` into Postman
2. Set environment variable: `base_url = http://localhost:8080`
3. Update file paths in upload requests to match your system paths
4. Run individual requests or use Collection Runner for batch testing

#### Key Test Scenarios:
- **Import Testing**: Upload CSV/NDJSON files with validation edge cases
- **Stream Exports**: Real-time data export with filtering and pagination
- **Async Jobs**: Large dataset processing with status monitoring  
- **Error Handling**: Invalid payloads, unsupported formats, rate limiting
- **End-to-End Workflows**: Complete import → process → export → download cycles

### Running Tests

#### Unit Tests
```bash
# Run all unit tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/validation/...
```

#### Integration Testing with Postman
```bash
# Start the server
go run cmd/server/main.go

# Use Postman Collection Runner to execute all test scenarios
# Or run individual requests from the collection
```

#### Manual API Testing
```bash
# Test user import with validation errors
curl -X POST http://localhost:8080/v1/imports \
  -H "Idempotency-Key: test-users-001" \
  -F "file=@import_testdata_all_in_one/users_huge.csv" \
  -F "resource=users" \
  -F "format=csv"

# Test article export with filtering
curl "http://localhost:8080/v1/exports?resource=articles&format=ndjson&status=published&limit=100" > articles.ndjson

# Monitor job progress
curl http://localhost:8080/v1/imports/{job_id}
```

#### Performance Testing
The test datasets are sized for performance validation:
- **Users**: 10K records → ~1-2 seconds import time
- **Articles**: 15K records → ~3-5 seconds import time  
- **Comments**: 20K records → ~4-6 seconds import time
- **Export**: Should achieve 5K+ rows/sec throughput

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