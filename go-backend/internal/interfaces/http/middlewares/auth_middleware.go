package middlewares

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
	"university-result-processing/backend/internal/pkg/crypto"
)

func AuthMiddleware(jwtSecret string, userRepo domain.UserRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		var tokenStr string

		if authHeader == "" {
			tokenStr = c.Query("token")
			if tokenStr == "" {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is required"})
				c.Abort()
				return
			}
		} else {
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header format must be Bearer {token}"})
				c.Abort()
				return
			}
			tokenStr = parts[1]
		}
		claims, err := crypto.ParseToken(tokenStr, jwtSecret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			c.Abort()
			return
		}

		// Verify user status
		// Using parent context (no tenant ID set yet)
		user, err := userRepo.GetByID(c.Request.Context(), claims.UserID)
		if err != nil || user == nil || user.Status != "active" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User suspended or inactive"})
			c.Abort()
			return
		}

		// Set variables in Gin Context
		c.Set("user_id", claims.UserID)
		c.Set("tenant_id", claims.TenantID)
		c.Set("roles", claims.Roles)

		// Inject into Go Context for downstream repositories
		goCtx := c.Request.Context()
		goCtx = contextutil.WithTenantID(goCtx, claims.TenantID)
		goCtx = contextutil.WithUserID(goCtx, claims.UserID)
		c.Request = c.Request.WithContext(goCtx)

		c.Next()
	}
}

func RequirePermission(requiredPermission string, userRepo domain.UserRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDVal, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		userID := userIDVal.(uuid.UUID)
		_, permissions, err := userRepo.GetUserRolesAndPermissions(c.Request.Context(), userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify permissions"})
			c.Abort()
			return
		}

		hasPerm := false
		for _, perm := range permissions {
			if perm == requiredPermission {
				hasPerm = true
				break
			}
		}

		if !hasPerm {
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: insufficient permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}
