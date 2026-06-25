package main

import (
	"database/sql"
	"log"
	"os"

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
		log.Fatalf("Failed to ping database on port 5433. Make sure container is started: %v", err)
	}

	// Read migration SQL
	migrationPath := "./migrations/000001_init_schema.up.sql"
	sqlContent, err := os.ReadFile(migrationPath)
	if err != nil {
		// Try absolute path resolution if working dir differs
		migrationPath = "../../migrations/000001_init_schema.up.sql"
		sqlContent, err = os.ReadFile(migrationPath)
		if err != nil {
			log.Fatalf("Failed to read migration SQL file: %v", err)
		}
	}

	log.Printf("Executing migration: %s", migrationPath)
	_, err = db.Exec(string(sqlContent))
	if err != nil {
		log.Fatalf("Migration execution failed: %v", err)
	}

	log.Println("Database schema migrations completed successfully! All tables created.")
}
