package main

import (
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func main() {
	// Initialize LogTide.
	flush := logtide.Init(logtide.ClientOptions{
		DSN:     "https://lp_your_api_key_here@api.logtide.dev",
		Service: "echo-example",
	})
	defer flush()

	client := logtide.CurrentHub().Client()

	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(LogtideMiddleware(client))

	e.GET("/", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"message": "Hello from Echo!",
		})
	})

	e.GET("/user/:id", func(c echo.Context) error {
		userID := c.Param("id")
		client.Info(c.Request().Context(), "Fetching user details", map[string]any{
			"user_id": userID,
		})
		return c.JSON(http.StatusOK, map[string]any{
			"user_id": userID,
			"name":    "Jane Doe",
		})
	})

	e.POST("/login", func(c echo.Context) error {
		type LoginRequest struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}

		var req LoginRequest
		if err := c.Bind(&req); err != nil {
			client.Error(c.Request().Context(), "Invalid login request", map[string]any{
				"error": err.Error(),
			})
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		client.Info(c.Request().Context(), "User login attempt", map[string]any{
			"username": req.Username,
			"success":  true,
		})
		return c.JSON(http.StatusOK, map[string]any{
			"message": "Login successful",
			"token":   "sample-jwt-token",
		})
	})

	e.GET("/error", func(c echo.Context) error {
		client.Error(c.Request().Context(), "Simulated error endpoint", map[string]any{
			"endpoint": "/error",
			"ip":       c.RealIP(),
		})
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Internal server error",
		})
	})

	log.Println("Starting Echo server on :8080")
	if err := e.Start(":8080"); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// LogtideMiddleware logs each HTTP request to LogTide.
func LogtideMiddleware(client *logtide.Client) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			handlerErr := next(c)
			duration := time.Since(start)

			statusCode := c.Response().Status
			if handlerErr != nil {
				if he, ok := handlerErr.(*echo.HTTPError); ok {
					statusCode = he.Code
				} else {
					statusCode = http.StatusInternalServerError
				}
			}

			metadata := map[string]any{
				"method":       c.Request().Method,
				"path":         c.Request().URL.Path,
				"status":       statusCode,
				"duration_ms":  duration.Milliseconds(),
				"ip":           c.RealIP(),
				"user_agent":   c.Request().UserAgent(),
				"query_params": c.QueryParams().Encode(),
			}
			if handlerErr != nil {
				metadata["error"] = handlerErr.Error()
			}

			msg := "HTTP request completed"
			switch {
			case statusCode >= 500:
				client.Error(c.Request().Context(), msg, metadata)
			case statusCode >= 400:
				client.Warn(c.Request().Context(), msg, metadata)
			default:
				client.Info(c.Request().Context(), msg, metadata)
			}

			return handlerErr
		}
	}
}
