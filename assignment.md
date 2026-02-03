Coding Assignment - Bulk Data Import/Export (v1.0)
Build endpoints to import and export articles, comments, and users in JSON/NDJSON. The system must scale to large datasets, validate rigorously, secure operations, and report errors clearly.

What We're Looking For
Efficient large-dataset handling (streamed responses; background jobs for imports)
Robust validation with per-record error capture (continue on error)
Clear errors and logging (actionable messages; structured logs/metrics) (use prometheus for logs and metrics)
API Surface (minimum)
Import (Async Job)
POST /v1/imports
Accepts file upload (multipart) or JSON body with a remote file URL.
Use Idempotency-Key header to avoid duplicate processing.

GET /v1/imports/{job_id} returns job status, counters, and errors.

Export (Streaming or Async)
GET /v1/exports?resource=articles&format=ndjson streams data.
POST /v1/exports for async export with filters and fields.
GET /v1/exports/{job_id} to retrieve export status and download URL.

Data Schemas
users: id, email, name, role, active, created_at, updated_at
articles: id, slug, title, body, author_id, tags, published_at, status
comments: id, article_id, user_id, body, created_at

Validation Rules
Users: valid and unique email; allowed roles; boolean active
Articles: valid author_id; slug unique and kebab-case; draft must not have published_at
Comments: valid foreign keys; body length <= 500 words
Upsert requires id or natural key (email, slug)
Foreign key errors are recorded and skipped
Performance Expectations
Handle up to 1,000,000 records per job
Stream processing (O(1) memory)
NDJSON export: 5k rows/sec
Batch writes (1k records)
Observability: log rows/sec, error_rate, duration
Acceptance Criteria
Streamed export and background import jobs
Strong per-record validation + error reporting
Structured error messages and logs
Automated tests (unit and integration tests as a bonus)
Comprehensive README (setup instructions, environment configuration, API documentation, dependencies, and usage examples)
Recommended Starter Templates

To help you focus on design and implementation quality rather than boilerplate setup, you may use one of the following existing starter projects:

Go: golang-gin-realworld-example-app
A well-structured Go backend that demonstrates clean architecture patterns, RESTful design, and scalable code organization.

Node.js: node-express-realworld-example-app
A production-grade Node.js/Express starter with authentication, routing, and testing already set up — ideal for building the import/export APIs quickly.

Using one of these templates is optional but recommended if you want to demonstrate how your bulk import/export feature integrates into a realistic application structure.

You should also consider using Docker and LocalStack to set up the local environment and enable local testing of the application.

Test data
You can find the test data in folder import_testdata_all_in_one at the same level as this file. This data will be used to evaluate. 

Follow-up Questions & Submission
Once your application is ready, share it on GitHub and submit it here. You can also use the same form for any follow-up questions.