package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"university-result-processing/backend/internal/pkg/crypto"
)

func main() {
	log.Println("Starting database seeding process...")

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres_secure_db_pass_2026@127.0.0.1:5439/university_ocr?sslmode=disable"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping DB: %v", err)
	}

	ctx := context.Background()

	// 1. Seed Tenant
	tenantID := uuid.MustParse("e93fca1e-1f7c-47bc-87c2-127e7740e53a")
	_, err = db.ExecContext(ctx, `
		INSERT INTO tenants (id, name, domain, settings)
		VALUES ($1, 'Micronic Infotech Services Private Limited', 'micronicinfo.com', '{}'::jsonb)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, domain = EXCLUDED.domain
	`, tenantID)
	if err != nil {
		log.Fatalf("Failed to seed tenant: %v", err)
	}
	log.Println("Tenant seeded.")

	// 2. Seed User
	userID := uuid.MustParse("c869fb1e-cfa1-4560-9bb3-5bb28e2195f2")
	// Password is "PasswordArgon2!12" hashed
	passHash, err := crypto.HashPassword("PasswordArgon2!12")
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, tenant_id, email, password_hash, first_name, last_name, status)
		VALUES ($1, $2, 'admin@micronicinfo.com', $3, 'Admin', 'User', 'active')
		ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, password_hash = EXCLUDED.password_hash, status = EXCLUDED.status
	`, userID, tenantID, passHash)
	if err != nil {
		log.Fatalf("Failed to seed user: %v", err)
	}
	log.Println("User seeded.")

	// 3. Seed Document
	docID := uuid.Nil // 00000000-0000-0000-0000-000000000000
	_, err = db.ExecContext(ctx, `
		INSERT INTO documents (id, tenant_id, name, file_path, file_size, mime_type, status, uploaded_by)
		VALUES ($1, $2, 'semester_results_fall_2026.pdf', 'uploads/semester_results_fall_2026.pdf', 1048576, 'application/pdf', 'extracted', $3)
		ON CONFLICT (id) DO NOTHING
	`, docID, tenantID, userID)
	if err != nil {
		log.Fatalf("Failed to seed document: %v", err)
	}
	log.Println("Document seeded.")

	// 4. Seed Document Page
	pageID := uuid.MustParse("f512fb1e-355b-426b-a8ba-88cfce814ff2")
	_, err = db.ExecContext(ctx, `
		INSERT INTO document_pages (id, document_id, page_number, image_path, width, height, status)
		VALUES ($1, $2, 1, 'micronicinfo.com/demo_page.png', 800, 1100, 'processed')
		ON CONFLICT (document_id, page_number) DO NOTHING
	`, pageID, docID)
	if err != nil {
		log.Fatalf("Failed to seed page: %v", err)
	}
	log.Println("Page seeded.")

	// 5. Seed Extraction
	extID := uuid.New()
	_, err = db.ExecContext(ctx, `
		INSERT INTO extractions (id, tenant_id, document_id, status)
		VALUES ($1, $2, $3, 'completed')
		ON CONFLICT (id) DO NOTHING
	`, extID, tenantID, docID)
	if err != nil {
		log.Fatalf("Failed to seed extraction: %v", err)
	}

	// 6. Seed Table
	tableID := uuid.New()
	_, err = db.ExecContext(ctx, `
		INSERT INTO extracted_tables (id, extraction_id, page_number, table_index, bounding_box)
		VALUES ($1, $2, 1, 0, '{"x": 50, "y": 100, "width": 700, "height": 800}'::jsonb)
		ON CONFLICT (id) DO NOTHING
	`, tableID, extID)
	if err != nil {
		log.Fatalf("Failed to seed table: %v", err)
	}

	// 7. Seed Rows & Cells
	headers := []string{"Roll No", "Student Name", "Marks Obtained", "Result Status"}
	rowsData := [][]struct {
		Val  string
		Conf float64
		X    float64
		Y    float64
		W    float64
		H    float64
	}{
		// Row 1
		{
			{"1001", 0.99, 80, 200, 100, 30},
			{"Alice Smith", 0.98, 200, 200, 250, 30},
			{"85", 0.99, 470, 200, 100, 30},
			{"Pass", 0.99, 590, 200, 100, 30},
		},
		// Row 2
		{
			{"1002", 0.99, 80, 250, 100, 30},
			{"Bob Johnson", 0.97, 200, 250, 250, 30},
			{"90", 0.99, 470, 250, 100, 30},
			{"Pass", 0.99, 590, 250, 100, 30},
		},
		// Row 3
		{
			{"1003", 0.99, 80, 300, 100, 30},
			{"Charlie Brown", 0.95, 200, 300, 250, 30},
			{"8S", 0.65, 470, 300, 100, 30}, // Low confidence HTR error!
			{"Pass", 0.99, 590, 300, 100, 30},
		},
	}

	// Insert headers row 0
	row0ID := uuid.New()
	_, _ = db.ExecContext(ctx, "INSERT INTO extracted_rows (id, table_id, row_index) VALUES ($1, $2, 0)", row0ID, tableID)
	for colIdx, hVal := range headers {
		cellID := uuid.New()
		bbox := map[string]float64{"x": float64(80 + colIdx*120), "y": 150, "width": 100, "height": 30}
		bboxJSON, _ := json.Marshal(bbox)
		_, _ = db.ExecContext(ctx, `
			INSERT INTO extracted_cells (id, document_id, row_index, column_index, original_value, current_value, confidence, bbox, version)
			VALUES ($1, $2, 0, $3, $4, $5, 0.99, $6, 1)
		`, cellID, docID, colIdx, hVal, hVal, bboxJSON)
	}

	// Insert data rows 1-3
	for rIdx, row := range rowsData {
		dbRowIdx := rIdx + 1
		rowID := uuid.New()
		_, _ = db.ExecContext(ctx, "INSERT INTO extracted_rows (id, table_id, row_index) VALUES ($1, $2, $3)", rowID, tableID, dbRowIdx)
		
		for colIdx, cell := range row {
			cellID := uuid.New()
			bbox := map[string]float64{"x": cell.X, "y": cell.Y, "width": cell.W, "height": cell.H}
			bboxJSON, _ := json.Marshal(bbox)
			_, _ = db.ExecContext(ctx, `
				INSERT INTO extracted_cells (id, document_id, row_index, column_index, original_value, current_value, confidence, bbox, version)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1)
			`, cellID, docID, dbRowIdx, colIdx, cell.Val, cell.Val, cell.Conf, bboxJSON)
		}
	}

	// Seed some base roles/permissions
	_, _ = db.ExecContext(ctx, "INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name = 'System Admin'", userID)

	log.Println("Database successfully seeded with demo documents and OCR verification grid!")
}
