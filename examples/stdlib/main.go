package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	logtide "github.com/logtide-dev/logtide-sdk-go"
	"github.com/logtide-dev/logtide-sdk-go/integrations/nethttp"
)

func main() {
	// Initialize LogTide.
	flush := logtide.Init(logtide.ClientOptions{
		DSN:     "https://lp_your_api_key_here@api.logtide.dev",
		Service: "stdlib-example",
	})
	defer flush()

	client := logtide.CurrentHub().Client()

	// Create router.
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Hello from standard library!",
		})
	})

	mux.HandleFunc("/user/", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Path[len("/user/"):]

		client.Info(r.Context(), "Fetching user details", map[string]any{
			"user_id": userID,
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"user_id": userID,
			"name":    "Alice Smith",
		})
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		type LoginRequest struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			client.Error(r.Context(), "Invalid login request", map[string]any{
				"error": err.Error(),
			})
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		client.Info(r.Context(), "User login attempt", map[string]any{
			"username": req.Username,
			"success":  true,
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": "Login successful",
			"token":   "sample-jwt-token",
		})
	})

	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		client.Error(r.Context(), "Simulated error endpoint", map[string]any{
			"endpoint": "/error",
			"ip":       r.RemoteAddr,
		})
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	})

	// Wrap with the LogTide net/http middleware — injects Hub into each request context
	// and enriches it with HTTP metadata (method, path, IP, traceparent header).
	handler := nethttp.Middleware(mux)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Println("Starting HTTP server on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start server: %v", err)
	}
}
