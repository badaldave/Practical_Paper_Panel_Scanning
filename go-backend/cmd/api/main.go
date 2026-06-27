package main

import (
	"context"
	"crypto/rand"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/application/auth"
	"university-result-processing/backend/internal/application/document"
	"university-result-processing/backend/internal/application/extraction"
	"university-result-processing/backend/internal/application/role"
	"university-result-processing/backend/internal/application/settings"
	"university-result-processing/backend/internal/application/stats"
	"university-result-processing/backend/internal/application/user"
	"university-result-processing/backend/internal/application/verification"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/infrastructure/db"
	"university-result-processing/backend/internal/infrastructure/storage"
	httpRouter "university-result-processing/backend/internal/interfaces/http"
	"university-result-processing/backend/internal/interfaces/http/handlers"
	"university-result-processing/backend/internal/pkg/crypto"
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
	roleRepo := db.NewRoleRepository(database)
	verifRepo := db.NewVerificationRepository(database)
	statsRepo := db.NewStatsRepository(database)

	// Seed permission catalog, shared system roles, and their grants if needed
	seedRolesAndPermissions(database)

	// Bootstrap a tenant + first admin on a fresh database so the platform is
	// immediately usable without a separate `cmd/seed` run. No-op once any user
	// exists, so a rotated admin password is never clobbered on restart.
	bootstrapAdmin(database)

	// 5. Initialize Services
	authService := auth.NewAuthService(tenantRepo, userRepo, jwtSecret)
	docService := document.NewDocumentService(docRepo, queueRepo, store)
	extService := extraction.NewExtractionService(extRepo, docRepo, auditRepo)
	userService := user.NewUserService(userRepo)
	roleService := role.NewRoleService(roleRepo)
	verifService := verification.NewVerificationService(verifRepo)
	statsService := stats.NewStatsService(statsRepo)
	settingsService := settings.NewSettingsService(tenantRepo)

	// 6. Initialize Handlers
	authHandler := handlers.NewAuthHandler(authService)
	docHandler := handlers.NewDocumentHandler(docService)
	extHandler := handlers.NewExtractionHandler(extService)
	tmplHandler := handlers.NewTemplateHandler(tmplRepo)
	exportHandler := handlers.NewExportHandler(docRepo, extRepo, auditRepo, settingsService)
	userHandler := handlers.NewUserHandler(userService)
	roleHandler := handlers.NewRoleHandler(roleService)
	verifHandler := handlers.NewVerificationHandler(verifService)
	statsHandler := handlers.NewStatsHandler(statsService)
	settingsHandler := handlers.NewSettingsHandler(settingsService)

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
		AuthHandler:         authHandler,
		DocumentHandler:     docHandler,
		ExtractionHandler:   extHandler,
		TemplateHandler:     tmplHandler,
		ExportHandler:       exportHandler,
		UserHandler:         userHandler,
		RoleHandler:         roleHandler,
		VerificationHandler: verifHandler,
		StatsHandler:        statsHandler,
		SettingsHandler:     settingsHandler,
		JWTSecret:           jwtSecret,
		UserRepository:      userRepo,
		RateLimitRate:       5.0,  // 5 requests per second
		RateLimitCap:        20.0, // Burst capacity of 20
		UploadDir:           uploadDir,
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

// allPermissions is the platform's permission catalog. Codes are
// "<category>.<action>"; the frontend groups by category.
var allPermissions = []struct{ Code, Desc string }{
	{"documents.view", "View documents and extracted data"},
	{"documents.upload", "Upload new documents for processing"},
	{"documents.delete", "Delete documents"},
	{"documents.export", "Export extracted data (CSV/Excel)"},
	{"verification.perform", "Claim files and verify/correct cell data"},
	{"verification.assign", "Assign files to verifiers and force-release locks"},
	{"users.view", "View users"},
	{"users.manage", "Create, edit, delete users and reset passwords"},
	{"roles.view", "View roles and permissions"},
	{"roles.manage", "Create, edit, and delete roles"},
	{"analytics.view", "View analytics, statistics, and live activity"},
	{"templates.view", "View document templates"},
	{"templates.manage", "Create and edit document templates"},
	{"settings.manage", "Manage tenant settings"},
}

// allPermissionCodes returns every catalog code (used to grant System Admin all).
func allPermissionCodes() []string {
	codes := make([]string, len(allPermissions))
	for i, p := range allPermissions {
		codes[i] = p.Code
	}
	return codes
}

// systemRoles are the shared, non-editable roles available to every tenant,
// each with its default permission grant set.
var systemRoles = []struct {
	Name, Description string
	Permissions       []string
}{
	{"System Admin", "Full access to every feature, user, and setting", allPermissionCodes()},
	{"Controller of Examinations", "Oversees verification, assigns work, views analytics",
		[]string{"documents.view", "documents.export", "verification.perform", "verification.assign", "analytics.view", "users.view"}},
	{"Registrar", "Reviews statistics, users, and audit records",
		[]string{"documents.view", "analytics.view", "users.view", "roles.view"}},
	{"Evaluator", "Verifies and corrects extracted data from the pool",
		[]string{"documents.view", "verification.perform"}},
	{"Viewer", "Read-only access to documents", []string{"documents.view"}},
}

func seedRolesAndPermissions(database *db.DB) {
	// 1. Permission catalog (code is uniquely constrained).
	for _, p := range allPermissions {
		if _, err := database.Exec(`
			INSERT INTO permissions (id, code, description)
			VALUES (uuid_generate_v4(), $1, $2)
			ON CONFLICT (code) DO UPDATE SET description = EXCLUDED.description
		`, p.Code, p.Desc); err != nil {
			log.Printf("seed permission %s failed: %v", p.Code, err)
		}
	}

	// 2. Shared system roles (tenant_id IS NULL, is_system = TRUE). We can't use
	//    ON CONFLICT against the expression-based uniqueness index, so insert-if-
	//    absent then normalize.
	for _, r := range systemRoles {
		if _, err := database.Exec(`
			INSERT INTO roles (id, tenant_id, name, description, is_system)
			SELECT uuid_generate_v4(), NULL, $1, $2, TRUE
			WHERE NOT EXISTS (SELECT 1 FROM roles WHERE tenant_id IS NULL AND name = $1)
		`, r.Name, r.Description); err != nil {
			log.Printf("seed role %s failed: %v", r.Name, err)
		}
		_, _ = database.Exec(`UPDATE roles SET is_system = TRUE, description = $2 WHERE tenant_id IS NULL AND name = $1`, r.Name, r.Description)

		// 3. Grant the role's permissions (idempotent).
		if _, err := database.Exec(`
			INSERT INTO role_permissions (role_id, permission_id)
			SELECT r.id, p.id FROM roles r, permissions p
			WHERE r.tenant_id IS NULL AND r.name = $1 AND p.code = ANY($2)
			ON CONFLICT DO NOTHING
		`, r.Name, r.Permissions); err != nil {
			log.Printf("grant permissions for role %s failed: %v", r.Name, err)
		}
	}
}

// bootstrapTenantID is the canonical single-tenant ID used by both this
// startup bootstrap and cmd/seed, so they converge on one tenant rather than
// creating duplicates.
const bootstrapTenantID = "e93fca1e-1f7c-47bc-87c2-127e7740e53a"

// bootstrapAdmin ensures the platform is usable on a fresh database: it creates
// the default tenant (if absent) and, only when the tenant has no users yet, a
// System Admin user. Credentials come from SEED_ADMIN_EMAIL/SEED_ADMIN_PASSWORD;
// an unset password is randomly generated and printed once. It is idempotent —
// once any user exists it does nothing, so restarts never overwrite a password
// the operator has since changed.
func bootstrapAdmin(database *db.DB) {
	tenantID := uuid.MustParse(bootstrapTenantID)

	tenantName := strings.TrimSpace(os.Getenv("SEED_TENANT_NAME"))
	if tenantName == "" {
		tenantName = "Micronic Infotech Services Private Limited"
	}
	tenantDomain := strings.TrimSpace(strings.ToLower(os.Getenv("SEED_TENANT_DOMAIN")))
	if tenantDomain == "" {
		tenantDomain = "micronicinfo.com"
	}

	// Create the tenant if it isn't there. DO NOTHING keeps an existing tenant's
	// name/domain intact across restarts.
	if _, err := database.Exec(`
		INSERT INTO tenants (id, name, domain, settings)
		VALUES ($1, $2, $3, '{}'::jsonb)
		ON CONFLICT (id) DO NOTHING
	`, tenantID, tenantName, tenantDomain); err != nil {
		log.Printf("bootstrap tenant failed: %v", err)
		return
	}

	// Only seed an admin when this tenant has no users at all.
	var userCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM users WHERE tenant_id = $1`, tenantID).Scan(&userCount); err != nil {
		log.Printf("bootstrap admin: user count check failed: %v", err)
		return
	}
	if userCount > 0 {
		return
	}

	adminEmail := strings.TrimSpace(strings.ToLower(os.Getenv("SEED_ADMIN_EMAIL")))
	if adminEmail == "" {
		adminEmail = "admin@" + tenantDomain
	}
	adminPassword := os.Getenv("SEED_ADMIN_PASSWORD")
	generated := false
	if adminPassword == "" {
		var err error
		adminPassword, err = randomPassword(20)
		if err != nil {
			log.Printf("bootstrap admin: password generation failed: %v", err)
			return
		}
		generated = true
	}

	passHash, err := crypto.HashPassword(adminPassword)
	if err != nil {
		log.Printf("bootstrap admin: password hashing failed: %v", err)
		return
	}

	userID := uuid.New()
	if _, err := database.Exec(`
		INSERT INTO users (id, tenant_id, email, password_hash, first_name, last_name, status)
		VALUES ($1, $2, $3, $4, 'Admin', 'User', 'active')
	`, userID, tenantID, adminEmail, passHash); err != nil {
		log.Printf("bootstrap admin: user insert failed: %v", err)
		return
	}

	// Grant System Admin (shared system role, tenant_id IS NULL).
	if _, err := database.Exec(`
		INSERT INTO user_roles (user_id, role_id)
		SELECT $1, id FROM roles WHERE tenant_id IS NULL AND name = 'System Admin'
		ON CONFLICT DO NOTHING
	`, userID); err != nil {
		log.Printf("bootstrap admin: role grant failed: %v", err)
	}

	log.Printf("Bootstrapped first admin (tenant domain: %s, email: %s).", tenantDomain, adminEmail)
	if generated {
		log.Printf("Generated admin password (shown once, store it now): %s", adminPassword)
	}
}

// randomPassword returns a cryptographically-random password of n characters
// satisfying the >=8 char password policy.
func randomPassword(n int) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*"
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b[i] = alphabet[idx.Int64()]
	}
	return string(b), nil
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
				ImagePath:  "micronicinfo.com/demo_page.png",
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
