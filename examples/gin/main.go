package main

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func main() {
	// Initialize LogTide.
	flush := logtide.Init(logtide.ClientOptions{
		DSN:     "https://lp_your_api_key_here@api.logtide.dev",
		Service: "gin-example",
	})
	defer flush()

	client := logtide.CurrentHub().Client()

	r := gin.Default()
	r.Use(LogtideMiddleware(client))

	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "Hello from Gin!"})
	})

	r.GET("/user/:id", func(c *gin.Context) {
		userID := c.Param("id")
		client.Info(c.Request.Context(), "Fetching user details", map[string]any{
			"user_id": userID,
		})
		c.JSON(200, gin.H{"user_id": userID, "name": "John Doe"})
	})

	r.POST("/login", func(c *gin.Context) {
		var body struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password" binding:"required"`
		}

		if err := c.ShouldBindJSON(&body); err != nil {
			client.Error(c.Request.Context(), "Invalid login request", map[string]any{
				"error": err.Error(),
			})
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		client.Info(c.Request.Context(), "User login attempt", map[string]any{
			"username": body.Username,
			"success":  true,
		})
		c.JSON(200, gin.H{
			"message": "Login successful",
			"token":   "sample-jwt-token",
		})
	})

	r.GET("/error", func(c *gin.Context) {
		client.Error(c.Request.Context(), "Simulated error endpoint", map[string]any{
			"endpoint": "/error",
			"ip":       c.ClientIP(),
		})
		c.JSON(500, gin.H{"error": "Internal server error"})
	})

	log.Println("Starting Gin server on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// LogtideMiddleware logs each HTTP request to LogTide.
func LogtideMiddleware(client *logtide.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)

		statusCode := c.Writer.Status()
		metadata := map[string]any{
			"method":       c.Request.Method,
			"path":         c.Request.URL.Path,
			"status":       statusCode,
			"duration_ms":  duration.Milliseconds(),
			"ip":           c.ClientIP(),
			"user_agent":   c.Request.UserAgent(),
			"query_params": c.Request.URL.RawQuery,
		}
		if len(c.Errors) > 0 {
			metadata["errors"] = c.Errors.String()
		}

		msg := "HTTP request completed"
		switch {
		case statusCode >= 500:
			client.Error(c.Request.Context(), msg, metadata)
		case statusCode >= 400:
			client.Warn(c.Request.Context(), msg, metadata)
		default:
			client.Info(c.Request.Context(), msg, metadata)
		}
	}
}
