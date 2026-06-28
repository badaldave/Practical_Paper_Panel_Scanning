package http

import (
	"github.com/gin-gonic/gin"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/interfaces/http/handlers"
	"university-result-processing/backend/internal/interfaces/http/middlewares"
)

type RouterConfig struct {
	AuthHandler         *handlers.AuthHandler
	DocumentHandler     *handlers.DocumentHandler
	ExtractionHandler   *handlers.ExtractionHandler
	TemplateHandler     *handlers.TemplateHandler
	ExportHandler       *handlers.ExportHandler
	UserHandler         *handlers.UserHandler
	RoleHandler         *handlers.RoleHandler
	VerificationHandler *handlers.VerificationHandler
	StatsHandler        *handlers.StatsHandler
	SettingsHandler     *handlers.SettingsHandler
	JWTSecret           string
	UserRepository      domain.UserRepository
	RateLimitRate       float64
	RateLimitCap        float64
	UploadDir           string
}

func SetupRouter(cfg RouterConfig) *gin.Engine {
	r := gin.New()

	// Global Middlewares
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(middlewares.CORSMiddleware())

	// Initialize Rate Limiter
	limiter := middlewares.NewRateLimiter(cfg.RateLimitRate, cfg.RateLimitCap)
	r.Use(limiter.Limit())

	// perm is a small helper for the per-route permission guard.
	perm := func(code string) gin.HandlerFunc {
		return middlewares.RequirePermission(code, cfg.UserRepository)
	}

	// Public Routes
	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/register", cfg.AuthHandler.Register)
			auth.POST("/login", cfg.AuthHandler.Login)
			auth.POST("/refresh", cfg.AuthHandler.Refresh)
		}
	}

	// Authenticated Scoped Routes
	authRequired := api.Group("")
	authRequired.Use(middlewares.AuthMiddleware(cfg.JWTSecret, cfg.UserRepository))
	{
		// Serve static uploaded document pages (outside /documents to avoid /:id conflict)
		authRequired.StaticFS("/uploads", gin.Dir(cfg.UploadDir, false))

		// Current user identity + live permissions
		authRequired.GET("/me", cfg.UserHandler.Me)

		// Documents
		docs := authRequired.Group("/documents")
		{
			docs.POST("", perm("documents.upload"), cfg.DocumentHandler.Upload)
			docs.GET("", perm("documents.view"), cfg.DocumentHandler.List)
			docs.GET("/:id", perm("documents.view"), cfg.DocumentHandler.GetByID)
			docs.DELETE("/:id", perm("documents.delete"), cfg.DocumentHandler.Delete)
			docs.DELETE("", perm("documents.delete"), cfg.DocumentHandler.DeleteAll)

			// Extracted cells & grids
			docs.GET("/:id/cells", perm("documents.view"), cfg.ExtractionHandler.GetActiveCells)
			docs.PUT("/:id/cells", perm("verification.perform"), cfg.ExtractionHandler.UpdateCell)
			docs.GET("/:id/cells/:row/:col/history", perm("documents.view"), cfg.ExtractionHandler.GetCellHistory)
			docs.DELETE("/:id/rows/:page/:row", perm("verification.perform"), cfg.ExtractionHandler.DeleteRow)
			docs.PUT("/:id/pages/:page_number", perm("verification.perform"), cfg.DocumentHandler.UpdatePageMetadata)

			// Export downloads
			docs.GET("/:id/export/csv", perm("documents.export"), cfg.ExportHandler.ExportCSV)
			docs.GET("/:id/export/excel", perm("documents.export"), cfg.ExportHandler.ExportExcel)
		}

		// Examiner directory lookup (mobile -> best-known name) for verification.
		authRequired.GET("/examiners/lookup", perm("verification.perform"), cfg.ExtractionHandler.LookupExaminer)

		// Verification pool, locking & per-page progress
		verif := authRequired.Group("/verification")
		{
			verif.GET("/queue", perm("verification.perform"), cfg.VerificationHandler.Queue)
			verif.GET("/:id/state", perm("verification.perform"), cfg.VerificationHandler.State)
			verif.POST("/:id/claim", perm("verification.perform"), cfg.VerificationHandler.Claim)
			verif.POST("/:id/release", perm("verification.perform"), cfg.VerificationHandler.Release)
			verif.POST("/:id/presence", perm("verification.perform"), cfg.VerificationHandler.Presence)
			verif.POST("/:id/pages/:page/verify", perm("verification.perform"), cfg.VerificationHandler.MarkPage)
			verif.POST("/:id/submit", perm("verification.perform"), cfg.VerificationHandler.Submit)
			// Admin overrides
			verif.POST("/:id/assign", perm("verification.assign"), cfg.VerificationHandler.Assign)
			verif.POST("/:id/force-release", perm("verification.assign"), cfg.VerificationHandler.ForceRelease)
		}

		// User management
		users := authRequired.Group("/users")
		{
			users.GET("", perm("users.view"), cfg.UserHandler.List)
			users.GET("/:id", perm("users.view"), cfg.UserHandler.Get)
			users.POST("", perm("users.manage"), cfg.UserHandler.Create)
			users.PUT("/:id", perm("users.manage"), cfg.UserHandler.Update)
			users.DELETE("/:id", perm("users.manage"), cfg.UserHandler.Delete)
			users.POST("/:id/reset-password", perm("users.manage"), cfg.UserHandler.ResetPassword)
		}

		// Role & permission management
		roles := authRequired.Group("/roles")
		{
			roles.GET("", perm("roles.view"), cfg.RoleHandler.List)
			roles.GET("/:id", perm("roles.view"), cfg.RoleHandler.Get)
			roles.POST("", perm("roles.manage"), cfg.RoleHandler.Create)
			roles.PUT("/:id", perm("roles.manage"), cfg.RoleHandler.Update)
			roles.DELETE("/:id", perm("roles.manage"), cfg.RoleHandler.Delete)
		}
		authRequired.GET("/permissions", perm("roles.view"), cfg.RoleHandler.ListPermissions)

		// Tenant settings — readable by any member, editable with settings.manage
		authRequired.GET("/settings", cfg.SettingsHandler.Get)
		authRequired.PUT("/settings", perm("settings.manage"), cfg.SettingsHandler.Update)

		// Analytics & monitoring
		anal := authRequired.Group("/analytics")
		anal.Use(perm("analytics.view"))
		{
			anal.GET("/overview", cfg.StatsHandler.Overview)
			anal.GET("/presence", cfg.StatsHandler.Presence)
			anal.GET("/activity", cfg.StatsHandler.Activity)
			anal.GET("/productivity", cfg.StatsHandler.Productivity)
			anal.GET("/timeseries", cfg.StatsHandler.Timeseries)
		}

		// Templates
		templates := authRequired.Group("/templates")
		{
			templates.POST("", perm("templates.manage"), cfg.TemplateHandler.Create)
			templates.GET("", perm("templates.view"), cfg.TemplateHandler.List)
			templates.POST("/:id/versions", perm("templates.manage"), cfg.TemplateHandler.CreateVersion)

			fields := templates.Group("/versions/:version_id/fields")
			{
				fields.POST("", perm("templates.manage"), cfg.TemplateHandler.CreateField)
				fields.GET("", perm("templates.view"), cfg.TemplateHandler.GetFields)
			}
		}
	}

	return r
}
