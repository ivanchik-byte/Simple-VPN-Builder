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
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/web"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	sharedmetrics "github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/metrics"
)

type Handlers struct {
	Auth         *handler.AuthHandler
	Node         *handler.NodeHandler
	User         *handler.UserHandler
	Plan         *handler.PlanHandler
	Credential   *handler.CredentialHandler
	Analytics    *handler.AnalyticsHandler
	Admin        *handler.AdminHandler
	Subscription *handler.SubscriptionHandler
	Billing      *handler.BillingHandler
	System       *handler.SystemHandler
	Web          *web.Handler
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
	r.Use(middleware.PrometheusMetrics)
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
		ExposedHeaders:   []string{"Link", "X-Request-ID", "RateLimit-Limit", "RateLimit-Remaining", "Subscription-Userinfo", "Profile-Update-Interval", "Profile-Title"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	if rateLimiter != nil {
		r.Use(rateLimiter.Middleware)
	}

	if authenticator != nil {
		r.Use(authenticator.Authenticate)
	}

	// Liveness and Readiness probes (rate-limiting is bypassed in rateLimiter.Middleware for these paths)
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
			stat := db.Stat()
			sharedmetrics.CPDBPoolConnsActive.Set(float64(stat.AcquiredConns()))
			sharedmetrics.CPDBPoolConnsIdle.Set(float64(stat.IdleConns()))
			sharedmetrics.CPDBPoolConnsMax.Set(float64(stat.MaxConns()))

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

	// Prometheus Metrics Endpoint
	r.Handle("/metrics", promhttp.Handler())

	// Public Universal Subscription Endpoint
	if handlers.Subscription != nil {
		r.Get("/sub/{token}", handlers.Subscription.GetSubscription)
	}

	// Redirect obsolete React SPA requests to unified Admin Web UI
	r.Get("/ui*", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})

	if handlers.Web != nil {
		r.NotFound(handlers.Web.NotFound)
		r.Get("/client/{token}", handlers.Web.ClientPortal)
		r.Post("/client/{token}/rotate", handlers.Web.RotateClientCredentials)
		r.Post("/client/{token}/reset", handlers.Web.RotateClientCredentials)
		r.Get("/client/{token}/connect", handlers.Web.ConnectDeepLink)
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
		})
		r.Handle("/admin/static/*", http.StripPrefix("/admin/", http.FileServer(http.FS(web.EmbeddedFiles))))
		r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/svg+xml")
			http.ServeContent(w, r, "favicon.svg", time.Time{}, web.FaviconBytes())
		})
		r.Head("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})

		// Public Web Auth routes
		r.Get("/admin/login", handlers.Web.LoginPage)
		r.Post("/admin/login", handlers.Web.Login)
		r.Post("/admin/logout", handlers.Web.Logout)

		// Protected Web Admin routes (Cookie-based JWT + CSRF protection)
		if authenticator != nil {
			r.Group(func(webRouter chi.Router) {
				webRouter.Use(web.RequireWebAuth(authenticator.JWTManager()))
				webRouter.Use(web.RequireCSRF(authenticator.JWTManager()))

				webRouter.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
				})
				webRouter.Get("/admin/dashboard", handlers.Web.Dashboard)
				webRouter.Get("/admin/partials/telemetry", handlers.Web.TelemetryPartial)
				webRouter.Get("/admin/qr", handlers.Web.GenerateQR)

				// Nodes
				webRouter.Get("/admin/nodes", handlers.Web.Nodes)
				webRouter.Post("/admin/nodes", handlers.Web.CreateNode)
				webRouter.Get("/admin/nodes/{id}", handlers.Web.NodeDetail)
				webRouter.Post("/admin/nodes/{id}/status", handlers.Web.UpdateNodeStatus)
				webRouter.Post("/admin/nodes/{id}/delete", handlers.Web.DeleteNode)

				// Users & Subscriptions
				webRouter.Get("/admin/users", handlers.Web.Users)
				webRouter.Post("/admin/users", handlers.Web.CreateUser)
				webRouter.Post("/admin/users/{id}/reset-traffic", handlers.Web.ResetUserTraffic)
				webRouter.Post("/admin/users/{id}/ban", handlers.Web.ToggleUserBan)
				webRouter.Post("/admin/users/{id}/delete", handlers.Web.DeleteUser)

				// Plans
				webRouter.Get("/admin/plans", handlers.Web.Plans)
				webRouter.Post("/admin/plans", handlers.Web.CreatePlan)
				webRouter.Post("/admin/plans/{id}", handlers.Web.UpdatePlan)
				webRouter.Post("/admin/plans/{id}/update", handlers.Web.UpdatePlan)
				webRouter.Post("/admin/plans/{id}/delete", handlers.Web.DeletePlan)

				// Credentials
				webRouter.Get("/admin/credentials", handlers.Web.Credentials)
				webRouter.Post("/admin/credentials/{id}/rotate", handlers.Web.RotateCredential)
				webRouter.Post("/admin/credentials/{id}/delete", handlers.Web.DeleteCredential)

				// Analytics & Audit Logs
				webRouter.Get("/admin/analytics", handlers.Web.Analytics)
				webRouter.Get("/admin/audit", handlers.Web.Audit)

				// Settings & Admins
				webRouter.Get("/admin/settings", handlers.Web.Settings)
				webRouter.Get("/admin/settings/billing", handlers.Web.SettingsBilling)
				webRouter.Post("/admin/settings/billing", handlers.Web.UpdateBillingSettings)
				webRouter.Get("/admin/broadcast", handlers.Web.BroadcastPage)
				webRouter.Post("/admin/broadcast", handlers.Web.CreateBroadcast)
				webRouter.Post("/admin/admins", handlers.Web.CreateAdmin)
				webRouter.Post("/admin/admins/{id}/delete", handlers.Web.DeleteAdmin)
				webRouter.Post("/admin/admins/{id}/permissions", handlers.Web.UpdateAdminPermissions)
				webRouter.Post("/admin/2fa/enable", handlers.Web.EnableTOTP)
				webRouter.Post("/admin/2fa/disable", handlers.Web.DisableTOTP)
				webRouter.Post("/admin/api-keys", handlers.Web.CreateAPIKey)
				webRouter.Post("/admin/api-keys/{id}/delete", handlers.Web.DeleteAPIKey)
				webRouter.Post("/admin/gateways", handlers.Web.UpdatePaymentGateway)
				webRouter.Post("/admin/telegram-bot/test", handlers.Web.TestTelegramBot)
			})

		}
	}

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
				ur.Post("/trial", handlers.User.CreateTrial)
				ur.Get("/by-telegram/{tg_id}", handlers.User.GetByTelegramID)
				ur.Get("/by-telegram/{tg_id}/referrals", handlers.User.GetReferralsByTelegramID)
				ur.Get("/{id}", handlers.User.Get)
				ur.Patch("/{id}", handlers.User.Update)
				ur.Delete("/{id}", handlers.User.Delete)
				ur.Post("/{id}/reset-traffic", handlers.User.ResetTraffic)
				ur.Get("/{id}/subscription", handlers.User.GetSubscription)
				ur.Post("/{id}/subscription/rotate", handlers.User.RotateSubscription)
				ur.Post("/{id}/rotate-keys", handlers.User.RotateKeys)
				ur.Post("/{id}/rotate", handlers.User.RotateKeys)
			})
		}

		if handlers.Plan != nil {
			apiRouter.Route("/plans", func(pr chi.Router) {
				pr.Use(middleware.RequireAuth)
				pr.Get("/", handlers.Plan.List)
				pr.Get("/trial", handlers.Plan.GetTrial)
				pr.Post("/", handlers.Plan.Create)
				pr.Get("/{id}", handlers.Plan.Get)
				pr.Patch("/{id}", handlers.Plan.Update)
				pr.Delete("/{id}", handlers.Plan.Delete)
			})
		}

		if handlers.Billing != nil {
			apiRouter.Route("/billing", func(br chi.Router) {
				// Public payment webhooks
				br.Post("/webhooks/{gateway}", handlers.Billing.ProcessWebhook)

				// Authenticated billing operations (Bot / Admin)
				br.Group(func(pr chi.Router) {
					pr.Use(middleware.RequireAuth)
					pr.Post("/invoices", handlers.Billing.CreateInvoice)
					pr.Post("/promos/validate", handlers.Billing.ValidatePromo)
					pr.Get("/gateways", handlers.Billing.ListGateways)
					pr.Put("/gateways", handlers.Billing.UpsertGateway)
					pr.Get("/settings", handlers.Billing.GetSettings)
					pr.Put("/settings", handlers.Billing.UpdateSettings)
					pr.Get("/broadcasts", handlers.Billing.ListBroadcasts)
					pr.Post("/broadcasts", handlers.Billing.CreateBroadcast)
					pr.Get("/broadcasts/{id}", handlers.Billing.GetBroadcast)
				})

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

		if handlers.System != nil {
			apiRouter.Route("/system", func(sr chi.Router) {
				sr.Use(middleware.RequireAuth)
				sr.Get("/telemetry", handlers.System.GetTelemetry)
			})
		}

		if handlers.Admin != nil {
			apiRouter.Route("/admins", func(ar chi.Router) {
				ar.Use(middleware.RequireAuth)
				ar.Use(middleware.RequireRole("superadmin"))
				ar.Get("/", handlers.Admin.ListAdmins)
				ar.Post("/", handlers.Admin.CreateAdmin)
			})
			apiRouter.Route("/api-keys", func(kr chi.Router) {
				kr.Use(middleware.RequireAuth)
				kr.Use(middleware.RequireRole("superadmin", "owner"))
				kr.Get("/", handlers.Admin.ListAPIKeys)
				kr.Post("/", handlers.Admin.CreateAPIKey)
				kr.Delete("/{id}", handlers.Admin.DeleteAPIKey)
			})
		}
	})

	return r
}
