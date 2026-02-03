Test Data Sets for Bulk Import Validation
----------------------------------------
users_huge.csv         : 10000 rows with header. Includes invalid emails, duplicate emails, missing IDs, invalid roles.
articles_huge.ndjson   : 15000 lines. Includes invalid slugs, duplicate slugs, invalid author_id FKs, drafts with published_at, missing IDs.
comments_huge.ndjson   : 20000 lines. Includes invalid article/user FKs, overly long bodies (>10k), empty bodies, missing 'body', missing IDs.

Conformance hints:
- CSV booleans are 'true'/'false'. Timestamps are ISO-8601 (Z).
- NDJSON: one JSON object per line.
- Majority of rows are valid; a small percentage are intentionally invalid to test validators and error reporting.
