# SQL Scripts

Database initialization scripts for PostgreSQL. Files are executed in alphabetical order on container first startup (via Docker's `docker-entrypoint-initdb.d` mechanism).

## What belongs in this folder:

```
sql/
├── 001_initial_schema.sql    # Main schema: jobs, entries, magic_cards tables
└── 002_add_indexes.sql       # Additional indexes (if needed later)
```

## Notes:
- Scripts run automatically when PostgreSQL container starts for the first time
- To re-run, delete the Docker volume: `docker compose down -v`
- Use `IF NOT EXISTS` clauses to make scripts idempotent
