package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/application/auth"
	"university-result-processing/backend/internal/application/document"
	"university-result-processing/backend/internal/application/extraction"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/infrastructure/db"
	"university-result-processing/backend/internal/infrastructure/storage"
	httpRouter "university-result-processing/backend/internal/interfaces/http"
	"university-result-processing/backend/internal/interfaces/http/handlers"
)

func main() {
	log.Println("Starting University Result Processing & Data Extraction API...")

	// 1. Configuration loading
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost port=5432 user=postgres password=postgres dbname=university_ocr sslmode=disable"
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "platform_jwt_secure_signing_secret_key_2026"
	}

	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./uploads"
	}

	// 2. Connect to database
	database, err := db.Connect(dsn)
	if err != nil {
		log.Fatalf("Failed to initialize database connection: %v", err)
	}
	defer database.Close()
	log.Println("Database connection established successfully.")

	// 3. Initialize Storage
	store, err := storage.NewLocalStorageProvider(uploadDir)
	if err != nil {
		log.Fatalf("Failed to initialize local storage: %v", err)
	}

	// 4. Initialize Repositories
	tenantRepo := db.NewTenantRepository(database)
	userRepo := db.NewUserRepository(database)
	docRepo := db.NewDocumentRepository(database)
	extRepo := db.NewExtractionRepository(database)
	tmplRepo := db.NewTemplateRepository(database)
	auditRepo := db.NewAuditRepository(database)
	queueRepo := db.NewQueueRepository(database)

	// Seed basic roles and permissions if needed
	seedRolesAndPermissions(database)

	// 5. Initialize Services
	authService := auth.NewAuthService(tenantRepo, userRepo, jwtSecret)
	docService := document.NewDocumentService(docRepo, queueRepo, store)
	extService := extraction.NewExtractionService(extRepo, docRepo, auditRepo)

	// 6. Initialize Handlers
	authHandler := handlers.NewAuthHandler(authService)
	docHandler := handlers.NewDocumentHandler(docService)
	extHandler := handlers.NewExtractionHandler(extService)
	tmplHandler := handlers.NewTemplateHandler(tmplRepo)
	exportHandler := handlers.NewExportHandler(docRepo, extRepo, auditRepo)

	// 7. Start local background mock processor for graceful standalone execution
	mockWorker := os.Getenv("MOCK_WORKER")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if mockWorker != "false" {
		log.Println("Starting mock job processor...")
		go startMockJobProcessor(ctx, queueRepo, docRepo, extRepo)
	} else {
		log.Println("Mock job processor disabled. Relying on external OCR daemon worker.")
	}

	// 8. Setup Router
	router := httpRouter.SetupRouter(httpRouter.RouterConfig{
		AuthHandler:       authHandler,
		DocumentHandler:   docHandler,
		ExtractionHandler: extHandler,
		TemplateHandler:   tmplHandler,
		ExportHandler:     exportHandler,
		JWTSecret:         jwtSecret,
		UserRepository:    userRepo,
		RateLimitRate:     5.0, // 5 requests per second
		RateLimitCap:      20.0, // Burst capacity of 20
		UploadDir:         uploadDir,
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	// Graceful Shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	log.Printf("Server listening on port %s", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	cancel() // Stop mock processor

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited cleanly")
}

func seedRolesAndPermissions(database *db.DB) {
	// Seed basic default roles
	roles := []struct {
		Name        string
		Description string
	}{
		{"System Admin", "Manage tenants, configuration, and overall settings"},
		{"Controller of Examinations", "Oversee and verify result lists"},
		{"Registrar", "Access statistics and audit records"},
		{"Evaluator", "Review extractions and correct values"},
		{"Viewer", "Read-only access to records"},
	}

	for _, r := range roles {
		_, _ = database.Exec(`
			INSERT INTO roles (id, name, description) 
			VALUES (uuid_generate_v4(), $1, $2)
			ON CONFLICT (name) DO NOTHING
		`, r.Name, r.Description)
	}
}

// startMockJobProcessor acts as a lightweight local polling worker in case the Python OCR daemon isn't running.
// This allows testing the backend, uploading files, seeing it process, and generating fake cells seamlessly.
func startMockJobProcessor(ctx context.Context, queueRepo domain.QueueRepository, docRepo domain.DocumentRepository, extRepo domain.ExtractionRepository) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Attempt to dequeue a pending job
			job, err := queueRepo.Dequeue(ctx, "local-mock-worker")
			if err != nil {
				continue
			}
			if job == nil {
				continue
			}

			log.Printf("[Mock Worker] Processing document: %s", job.DocumentID)

			// Record attempt
			attemptID := uuid.New()
			attempt := &domain.JobAttempt{
				ID:            attemptID,
				JobID:         job.ID,
				AttemptNumber: job.Attempts,
				StartedAt:     time.Now(),
				Status:        "processing",
			}
			_ = queueRepo.RecordAttempt(ctx, attempt)

			// Simulate processing time
			time.Sleep(3 * time.Second)

			// 1. Create document page
			pageID := uuid.New()
			_ = docRepo.CreatePage(ctx, &domain.DocumentPage{
				ID:         pageID,
				DocumentID: job.DocumentID,
				PageNumber: 1,
				ImagePath:  "test-uni.edu/demo_page.png",
				Width:      800,
				Height:     1100,
				Status:     "processed",
			})

			// 2. Mock cell extraction matching the marksheet coordinates
			extractionID := uuid.New()
			ext := &domain.Extraction{
				ID:         extractionID,
				TenantID:   job.TenantID,
				DocumentID: job.DocumentID,
				Status:     "completed",
			}
			_ = extRepo.CreateExtraction(ctx, ext)

			// Table
			tableID := uuid.New()
			_ = extRepo.CreateTable(ctx, &domain.ExtractedTable{
				ID:           tableID,
				ExtractionID: extractionID,
				PageNumber:   1,
				TableIndex:   0,
				BoundingBox:  domain.BoundingBox{X: 50, Y: 100, Width: 700, Height: 800},
			})

			// Headers
			headers := []string{"Roll No", "Student Name", "Marks Obtained", "Result Status"}
			
			// Mock data matching demo_page layout
			rowsData := [][]struct {
				Val  string
				Conf float64
				X    float64
				Y    float64
				W    float64
				H    float64
			}{
				{
					{"1001", 0.99, 80, 200, 100, 30},
					{"Alice Smith", 0.98, 200, 200, 250, 30},
					{"85", 0.99, 470, 200, 100, 30},
					{"Pass", 0.99, 590, 100, 100, 30},
				},
				{
					{"1002", 0.99, 80, 250, 100, 30},
					{"Bob Johnson", 0.97, 200, 250, 250, 30},
					{"90", 0.99, 470, 250, 100, 30},
					{"Pass", 0.99, 590, 250, 100, 30},
				},
				{
					{"1003", 0.99, 80, 300, 100, 30},
					{"Charlie Brown", 0.95, 200, 300, 250, 30},
					{"8S", 0.65, 470, 300, 100, 30},
					{"Pass", 0.99, 590, 300, 100, 30},
				},
			}

			// Save headers row 0
			_ = extRepo.CreateRow(ctx, &domain.ExtractedRow{ID: uuid.New(), TableID: tableID, RowIndex: 0})
			for colIdx, hVal := range headers {
				cellID := uuid.New()
				_ = extRepo.SaveCell(ctx, &domain.ExtractedCell{
					ID:            cellID,
					DocumentID:    job.DocumentID,
					RowIndex:      0,
					ColumnIndex:   colIdx,
					OriginalValue: hVal,
					CurrentValue:  hVal,
					Confidence:    0.99,
					BBox:          domain.BoundingBox{X: float64(80 + colIdx*120), Y: 150, Width: 100, Height: 30},
				})
			}

			// Save data rows 1-3
			for rIdx, row := range rowsData {
				dbRowIdx := rIdx + 1
				_ = extRepo.CreateRow(ctx, &domain.ExtractedRow{ID: uuid.New(), TableID: tableID, RowIndex: dbRowIdx})
				
				for colIdx, cell := range row {
					cellID := uuid.New()
					_ = extRepo.SaveCell(ctx, &domain.ExtractedCell{
						ID:            cellID,
						DocumentID:    job.DocumentID,
						RowIndex:      dbRowIdx,
						ColumnIndex:   colIdx,
						OriginalValue: cell.Val,
						CurrentValue:  cell.Val,
						Confidence:    cell.Conf,
						BBox:          domain.BoundingBox{X: cell.X, Y: cell.Y, Width: cell.W, Height: cell.H},
					})
				}
			}

			// Complete job
			_ = queueRepo.Complete(ctx, job.ID)
			_ = docRepo.UpdateStatus(ctx, job.DocumentID, "extracted")

			now := time.Now()
			_ = queueRepo.UpdateAttempt(ctx, attemptID, "completed", nil, now)
			log.Printf("[Mock Worker] Finished processing document: %s successfully.", job.DocumentID)
		}
	}
}
