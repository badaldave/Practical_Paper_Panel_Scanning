package main

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"sort"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	log.Println("Starting database migrations execution...")

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=127.0.0.1 port=5433 user=postgres password=postgres_secure_db_pass_2026 dbname=university_ocr sslmode=disable"
	}

	// Connect to DB
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("Failed to open database connection: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database. Make sure the container is started: %v", err)
	}

	// Resolve the migrations directory whether we're run from the repo root or
	// from within cmd/migrate.
	dir := "./migrations"
	if _, err := os.Stat(dir); err != nil {
		dir = "../../migrations"
	}

	// Apply every *.up.sql migration in ascending filename order. Each file is
	// authored to be idempotent, so re-running is safe.
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		log.Fatalf("Failed to list migration files: %v", err)
	}
	if len(files) == 0 {
		log.Fatalf("No migration files found in %s", dir)
	}
	sort.Strings(files)

	for _, path := range files {
		sqlContent, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("Failed to read migration %s: %v", path, err)
		}
		log.Printf("Executing migration: %s", filepath.Base(path))
		if _, err := db.Exec(string(sqlContent)); err != nil {
			log.Fatalf("Migration %s failed: %v", filepath.Base(path), err)
		}
	}

	log.Println("Database schema migrations completed successfully! All tables created/updated.")
}
