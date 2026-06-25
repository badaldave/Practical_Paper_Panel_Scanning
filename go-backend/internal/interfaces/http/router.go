package http

import (
	"github.com/gin-gonic/gin"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/interfaces/http/handlers"
	"university-result-processing/backend/internal/interfaces/http/middlewares"
)

type RouterConfig struct {
	AuthHandler      *handlers.AuthHandler
	DocumentHandler  *handlers.DocumentHandler
	ExtractionHandler *handlers.ExtractionHandler
	TemplateHandler  *handlers.TemplateHandler
	ExportHandler    *handlers.ExportHandler
	JWTSecret        string
	UserRepository   domain.UserRepository
	RateLimitRate    float64
	RateLimitCap     float64
	UploadDir        string
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

	// Public Routes
	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/register", cfg.AuthHandler.Register)
			auth.POST("/login", cfg.AuthHandler.Login)
		}
	}

	// Authenticated Scoped Routes
	authRequired := api.Group("")
	authRequired.Use(middlewares.AuthMiddleware(cfg.JWTSecret, cfg.UserRepository))
	{
		// Serve static uploaded document pages (outside /documents to avoid /:id conflict)
		authRequired.StaticFS("/uploads", gin.Dir(cfg.UploadDir, false))

		// Documents
		docs := authRequired.Group("/documents")
		{
			docs.POST("", cfg.DocumentHandler.Upload)
			docs.GET("", cfg.DocumentHandler.List)
			docs.GET("/:id", cfg.DocumentHandler.GetByID)
			docs.DELETE("/:id", cfg.DocumentHandler.Delete)
			docs.DELETE("", cfg.DocumentHandler.DeleteAll)
			
			// Extracted cells & grids
			docs.GET("/:id/cells", cfg.ExtractionHandler.GetActiveCells)
			docs.PUT("/:id/cells", cfg.ExtractionHandler.UpdateCell)
			docs.GET("/:id/cells/:row/:col/history", cfg.ExtractionHandler.GetCellHistory)
			docs.PUT("/:id/pages/:page_number", cfg.DocumentHandler.UpdatePageMetadata)
			
			// Export downloads
			docs.GET("/:id/export/csv", cfg.ExportHandler.ExportCSV)
			docs.GET("/:id/export/excel", cfg.ExportHandler.ExportExcel)
		}

		// Templates
		templates := authRequired.Group("/templates")
		{
			templates.POST("", cfg.TemplateHandler.Create)
			templates.GET("", cfg.TemplateHandler.List)
			templates.POST("/:id/versions", cfg.TemplateHandler.CreateVersion)
			
			fields := templates.Group("/versions/:version_id/fields")
			{
				fields.POST("", cfg.TemplateHandler.CreateField)
				fields.GET("", cfg.TemplateHandler.GetFields)
			}
		}
	}

	return r
}
