package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/handler"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Handlers struct {
	Auth       *handler.AuthHandler
	Node       *handler.NodeHandler
	User       *handler.UserHandler
	Plan       *handler.PlanHandler
	Credential *handler.CredentialHandler
	Analytics  *handler.AnalyticsHandler
	Admin      *handler.AdminHandler
}

// NewRouter builds the Chi router and mounts the global middleware chain and routes.
func NewRouter(
	cfg *config.Config,
	handlers Handlers,
	authenticator *middleware.Authenticator,
	rateLimiter *middleware.RateLimiter,
	db *pgxpool.Pool,
	rdb *redis.Client,
) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware pipeline
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.BodyLimit(1 << 20)) // 1 MB request body limit

	corsOrigins := []string{"http://localhost:3000", "http://localhost:8080"}
	if cfg != nil && len(cfg.Server.CORSAllowedOrigins) > 0 {
		corsOrigins = cfg.Server.CORSAllowedOrigins
	}

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID", "X-API-Key"},
		ExposedHeaders:   []string{"Link", "X-Request-ID", "RateLimit-Limit", "RateLimit-Remaining"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	if rateLimiter != nil {
		r.Use(rateLimiter.Middleware)
	}

	if authenticator != nil {
		r.Use(authenticator.Authenticate)
	}

	// Liveness and Readiness probes
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := HealthResponse{
			Status:   "ok",
			Postgres: "ok",
			Redis:    "ok",
		}
		statusCode := http.StatusOK

		checkCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if db != nil {
			if err := db.Ping(checkCtx); err != nil {
				resp.Postgres = "error: " + err.Error()
				resp.Status = "degraded"
				statusCode = http.StatusServiceUnavailable
			}
		}

		if rdb != nil {
			if err := rdb.Ping(checkCtx).Err(); err != nil {
				resp.Redis = "error: " + err.Error()
				resp.Status = "degraded"
				statusCode = http.StatusServiceUnavailable
			}
		}

		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(resp)
	})

	// API v1 routes
	r.Route("/api/v1", func(apiRouter chi.Router) {
		if handlers.Auth != nil {
			apiRouter.Route("/auth", func(authRouter chi.Router) {
				authRouter.Post("/login", handlers.Auth.Login)
				authRouter.Post("/refresh", handlers.Auth.Refresh)
				authRouter.Post("/logout", handlers.Auth.Logout)

				// Protected 2FA endpoints
				authRouter.Group(func(protected chi.Router) {
					protected.Use(middleware.RequireAuth)
					protected.Post("/totp/setup", handlers.Auth.SetupTOTP)
					protected.Post("/totp/verify", handlers.Auth.VerifyTOTP)
				})
			})
		}

		if handlers.Node != nil {
			apiRouter.Route("/nodes", func(nr chi.Router) {
				nr.Use(middleware.RequireAuth)
				nr.Get("/", handlers.Node.List)
				nr.Post("/", handlers.Node.Create)
				nr.Get("/{id}", handlers.Node.Get)
				nr.Patch("/{id}", handlers.Node.Update)
				nr.Delete("/{id}", handlers.Node.Delete)
				nr.Get("/{id}/stats", handlers.Node.GetStats)
			})
		}

		if handlers.User != nil {
			apiRouter.Route("/users", func(ur chi.Router) {
				ur.Use(middleware.RequireAuth)
				ur.Get("/", handlers.User.List)
				ur.Post("/", handlers.User.Create)
				ur.Get("/{id}", handlers.User.Get)
				ur.Patch("/{id}", handlers.User.Update)
				ur.Delete("/{id}", handlers.User.Delete)
				ur.Post("/{id}/reset-traffic", handlers.User.ResetTraffic)
				ur.Get("/{id}/subscription", handlers.User.GetSubscription)
				ur.Post("/{id}/subscription/rotate", handlers.User.RotateSubscription)
			})
		}

		if handlers.Plan != nil {
			apiRouter.Route("/plans", func(pr chi.Router) {
				pr.Use(middleware.RequireAuth)
				pr.Get("/", handlers.Plan.List)
				pr.Post("/", handlers.Plan.Create)
				pr.Get("/{id}", handlers.Plan.Get)
				pr.Patch("/{id}", handlers.Plan.Update)
				pr.Delete("/{id}", handlers.Plan.Delete)
			})
		}

		if handlers.Credential != nil {
			apiRouter.Route("/credentials", func(cr chi.Router) {
				cr.Use(middleware.RequireAuth)
				cr.Get("/", handlers.Credential.List)
				cr.Post("/", handlers.Credential.Create)
				cr.Get("/{id}", handlers.Credential.Get)
				cr.Delete("/{id}", handlers.Credential.Delete)
				cr.Post("/{id}/rotate", handlers.Credential.Rotate)
			})
		}

		if handlers.Analytics != nil {
			apiRouter.Route("/analytics", func(ar chi.Router) {
				ar.Use(middleware.RequireAuth)
				ar.Get("/overview", handlers.Analytics.Overview)
				ar.Get("/nodes", handlers.Analytics.GetByNode)
				ar.Get("/users/{id}", handlers.Analytics.GetByUser)
			})
		}

		if handlers.Admin != nil {
			apiRouter.Route("/admins", func(ar chi.Router) {
				ar.Use(middleware.RequireAuth)
				ar.Get("/", handlers.Admin.ListAdmins)
				ar.Post("/", handlers.Admin.CreateAdmin)
			})
			apiRouter.Route("/api-keys", func(kr chi.Router) {
				kr.Use(middleware.RequireAuth)
				kr.Get("/", handlers.Admin.ListAPIKeys)
				kr.Post("/", handlers.Admin.CreateAPIKey)
				kr.Delete("/{id}", handlers.Admin.DeleteAPIKey)
			})
		}
	})

	return r
}
