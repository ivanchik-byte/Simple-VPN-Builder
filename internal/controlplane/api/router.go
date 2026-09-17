package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/handler"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/web"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
	sharedmetrics "github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/metrics"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
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
	AI           *handler.AIHandler
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

	// Prometheus Metrics Endpoint (authenticated; scrape with JWT or API key)
	if authenticator != nil {
		r.With(middleware.RequireAuth).Handle("/metrics", promhttp.Handler())
	} else {
		r.Handle("/metrics", promhttp.Handler())
	}

	// Public Universal Subscription Endpoint
	if handlers.Subscription != nil {
		r.Get("/sub/{token}", handlers.Subscription.GetSubscription)
	}

	// Public node bootstrap installer (no secrets inside; node must be pre-created in panel).
	r.Get("/bootstrap/node.sh", web.BootstrapNodeScript)

	// (Legacy /ui SPA removed; assets are served under /ui/assets/* below.)

	if handlers.Web != nil {
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			// API clients always get RFC 7807 JSON, never the HTML error page.
			if strings.HasPrefix(r.URL.Path, "/api/") {
				response.RespondNotFound(w, r, "Endpoint not found")
				return
			}
			handlers.Web.NotFound(w, r)
		})
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
		})
		r.Handle("/admin/static/*", http.StripPrefix("/admin/", http.FileServer(http.FS(web.EmbeddedFiles))))
		// Fingerprinted bundles carry no secrets and must load pre-auth (login page).
		r.Handle("/ui/assets/*", web.DashboardAssets())
		r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/svg+xml")
			http.ServeContent(w, r, "favicon.svg", time.Time{}, web.FaviconBytes())
		})
		r.Head("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})

		// Public Web Auth routes
		r.Get("/admin/login", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/login-v2", http.StatusSeeOther)
		})
		r.Get("/admin/login-v2", web.DashboardV2().ServeHTTP)
		r.Post("/admin/login", handlers.Web.Login)

		// Protected Web Admin routes (Cookie-based JWT + CSRF protection)
		if authenticator != nil {
			r.Group(func(webRouter chi.Router) {
				webRouter.Use(web.RequireWebAuth(authenticator.JWTManager()))
				webRouter.Use(web.RequireCSRF(authenticator.JWTManager()))

				webRouter.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/dashboard-v2", http.StatusSeeOther)
				})
				uploadDir := "./data/uploads"
				_ = os.MkdirAll(uploadDir, 0755)
				webRouter.Handle("/admin/uploads/*", http.StripPrefix("/admin/uploads/", http.FileServer(http.Dir(uploadDir))))
				webRouter.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))
				webRouter.Get("/admin/dashboard", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/dashboard-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/nodes-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/users-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/plans-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/credentials-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/csrf-token", handlers.Web.CsrfToken)
				webRouter.Post("/admin/logout", handlers.Web.Logout)
				webRouter.Get("/admin/users-data", handlers.Web.UsersData)
				webRouter.Get("/admin/plans-data", handlers.Web.PlansData)
				webRouter.Get("/admin/analytics-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/audit-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/audit-data", handlers.Web.AuditData)
				webRouter.Get("/admin/broadcast-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/settings-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/node-v2", web.DashboardV2().ServeHTTP)
				webRouter.Get("/admin/node-data", handlers.Web.NodeData)
				webRouter.Get("/admin/settings-data", handlers.Web.SettingsData)
				webRouter.Get("/admin/qr", handlers.Web.GenerateQR)

				// Nodes
				webRouter.Get("/admin/nodes", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/nodes-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/nodes", handlers.Web.CreateNode)
				webRouter.Get("/admin/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/node-v2?id="+chi.URLParam(r, "id"), http.StatusSeeOther)
				})
				webRouter.Post("/admin/nodes/{id}/status", handlers.Web.UpdateNodeStatus)
				webRouter.Post("/admin/nodes/{id}/delete", handlers.Web.DeleteNode)

				// Users & Subscriptions
				webRouter.Get("/admin/users", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/users-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/users", handlers.Web.CreateUser)
				webRouter.Post("/admin/users/{id}/reset-traffic", handlers.Web.ResetUserTraffic)
				webRouter.Post("/admin/users/{id}/ban", handlers.Web.ToggleUserBan)
				webRouter.Post("/admin/users/{id}/delete", handlers.Web.DeleteUser)
				webRouter.Post("/admin/users/{id}/message", handlers.Web.DirectMessageUser)
				webRouter.Post("/admin/users/{id}/assign-plan", handlers.Web.AssignUserPlan)
				webRouter.Post("/admin/users/{id}/email", handlers.Web.UpdateUserEmail)

				// Plans
				webRouter.Get("/admin/plans", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/plans-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/plans", handlers.Web.CreatePlan)
				webRouter.Post("/admin/plans/{id}", handlers.Web.UpdatePlan)
				webRouter.Post("/admin/plans/{id}/update", handlers.Web.UpdatePlan)
				webRouter.Post("/admin/plans/{id}/delete", handlers.Web.DeletePlan)

				// Credentials
				webRouter.Get("/admin/credentials", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/credentials-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/credentials/{id}/rotate", handlers.Web.RotateCredential)
				webRouter.Post("/admin/credentials/{id}/delete", handlers.Web.DeleteCredential)

				// Analytics & Audit Logs
				webRouter.Get("/admin/analytics", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/analytics-v2", http.StatusSeeOther)
				})
				webRouter.Get("/admin/audit", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/audit-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/audit/purge", handlers.Web.PurgeOldAuditLogs)

				// Settings & Admins
				webRouter.Get("/admin/settings", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/settings-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/settings/retention", handlers.Web.UpdateLogRetentionSettings)
				webRouter.Post("/admin/settings/ai", handlers.Web.UpdateAISettings)
				webRouter.Post("/admin/settings/ai/test", handlers.Web.TestAIConnection)
				webRouter.Get("/admin/settings/billing", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/settings-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/settings/billing", handlers.Web.UpdateBillingSettings)
				webRouter.Get("/admin/settings/bot-replies", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/settings-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/settings/bot-replies", handlers.Web.UpdateBotReplies)
				webRouter.Get("/admin/settings/referrals", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/settings-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/settings/referrals", handlers.Web.UpdateReferralSettings)
				webRouter.Get("/admin/settings/security", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/settings-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/settings/security", handlers.Web.UpdateEmailPolicySettings)
				webRouter.Get("/admin/settings/partners", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/settings-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/settings/partners/create", handlers.Web.CreatePartnerTenant)
				webRouter.Post("/admin/settings/partners/{id}/delete", handlers.Web.DeletePartnerTenant)
				webRouter.Get("/admin/broadcast", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/broadcast-v2", http.StatusSeeOther)
				})
				webRouter.Post("/admin/broadcast", handlers.Web.CreateBroadcast)
				webRouter.Post("/admin/admins", handlers.Web.CreateAdmin)
				webRouter.Post("/admin/admins/{id}/delete", handlers.Web.DeleteAdmin)
				webRouter.Post("/admin/admins/{id}/permissions", handlers.Web.UpdateAdminPermissions)
				webRouter.Post("/admin/2fa/enable", handlers.Web.EnableTOTP)
				webRouter.Post("/admin/2fa/disable", handlers.Web.DisableTOTP)
				webRouter.Post("/admin/change-password", handlers.Web.ChangePassword)
				webRouter.Post("/admin/api-keys", handlers.Web.CreateAPIKey)
				webRouter.Post("/admin/api-keys/{id}/delete", handlers.Web.DeleteAPIKey)
				webRouter.Post("/admin/gateways", handlers.Web.UpdatePaymentGateway)
				webRouter.Post("/admin/gateways/create", handlers.Web.CreatePaymentGateway)
				webRouter.Post("/admin/gateways/{name}/toggle", handlers.Web.TogglePaymentGateway)
				webRouter.Post("/admin/gateways/{name}/delete", handlers.Web.DeletePaymentGateway)
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
				authRouter.Post("/change-password", handlers.Auth.ChangePassword)

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
				nr.With(middleware.RequireScope("node:read")).Get("/", handlers.Node.List)
				nr.With(middleware.RequireScope("node:write")).Post("/", handlers.Node.Create)
				nr.With(middleware.RequireScope("node:read")).Get("/{id}", handlers.Node.Get)
				nr.With(middleware.RequireScope("node:write")).Patch("/{id}", handlers.Node.Update)
				nr.With(middleware.RequireScope("node:write")).Delete("/{id}", handlers.Node.Delete)
				nr.With(middleware.RequireScope("node:read")).Get("/{id}/stats", handlers.Node.GetStats)
			})
		}

		if handlers.User != nil {
			apiRouter.Route("/users", func(ur chi.Router) {
				ur.Use(middleware.RequireAuth)
				ur.With(middleware.RequireScope("user:read")).Get("/", handlers.User.List)
				ur.With(middleware.RequireScope("user:write")).Post("/", handlers.User.Create)
				ur.With(middleware.RequireScope("user:write")).Post("/trial", handlers.User.CreateTrial)
				ur.With(middleware.RequireScope("user:write")).Post("/upsert-lead", handlers.User.UpsertTelegramLead)
				ur.With(middleware.RequireScope("user:write")).Post("/link-email", handlers.User.LinkTelegramEmail)
				ur.With(middleware.RequireScope("user:write")).Post("/restore-account", handlers.User.RestoreTelegramAccount)
				ur.With(middleware.RequireScope("user:read")).Post("/request-email-otp", handlers.User.RequestEmailOTP)
				ur.With(middleware.RequireScope("user:read")).Post("/verify-email-otp", handlers.User.VerifyEmailOTP)
				ur.With(middleware.RequireScope("user:read")).Get("/by-telegram/{tg_id}", handlers.User.GetByTelegramID)
				ur.With(middleware.RequireScope("user:read")).Get("/by-telegram/{tg_id}/referrals", handlers.User.GetReferralsByTelegramID)
				ur.With(middleware.RequireScope("user:read")).Get("/{id}", handlers.User.Get)
				ur.With(middleware.RequireScope("user:write")).Patch("/{id}", handlers.User.Update)
				ur.With(middleware.RequireScope("user:write")).Delete("/{id}", handlers.User.Delete)
				ur.With(middleware.RequireScope("user:write")).Post("/{id}/reset-traffic", handlers.User.ResetTraffic)
				ur.With(middleware.RequireScope("user:read")).Get("/{id}/subscription", handlers.User.GetSubscription)
				ur.With(middleware.RequireScope("user:write")).Post("/{id}/subscription/rotate", handlers.User.RotateSubscription)
				ur.With(middleware.RequireScope("user:write")).Post("/{id}/rotate-keys", handlers.User.RotateKeys)
				ur.With(middleware.RequireScope("user:write")).Post("/{id}/rotate", handlers.User.RotateKeys)
			})
		}

		if handlers.Plan != nil {
			apiRouter.Route("/plans", func(pr chi.Router) {
				pr.Use(middleware.RequireAuth)
				pr.With(middleware.RequireScope("billing:read")).Get("/", handlers.Plan.List)
				pr.With(middleware.RequireScope("billing:read")).Get("/trial", handlers.Plan.GetTrial)
				pr.With(middleware.RequireScope("billing:write")).Post("/", handlers.Plan.Create)
				pr.With(middleware.RequireScope("billing:read")).Get("/{id}", handlers.Plan.Get)
				pr.With(middleware.RequireScope("billing:write")).Patch("/{id}", handlers.Plan.Update)
				pr.With(middleware.RequireScope("billing:write")).Delete("/{id}", handlers.Plan.Delete)
			})
		}

		if handlers.Billing != nil {
			apiRouter.Route("/billing", func(br chi.Router) {
				// Public payment webhooks
				br.Post("/webhooks/{gateway}", handlers.Billing.ProcessWebhook)

				// Authenticated billing operations (Bot / Admin)
				br.Group(func(pr chi.Router) {
					pr.Use(middleware.RequireAuth)
					pr.With(middleware.RequireScope("billing:write")).Post("/invoices", handlers.Billing.CreateInvoice)
					pr.With(middleware.RequireScope("billing:write")).Post("/stars/confirm", handlers.Billing.ConfirmStarsPayment)
					pr.With(middleware.RequireScope("billing:read")).Post("/promos/validate", handlers.Billing.ValidatePromo)
					pr.With(middleware.RequireScope("billing:read")).Get("/gateways", handlers.Billing.ListGateways)
					pr.With(middleware.RequireScope("billing:write")).Put("/gateways", handlers.Billing.UpsertGateway)
					pr.With(middleware.RequireScope("billing:read")).Get("/settings", handlers.Billing.GetSettings)
					pr.With(middleware.RequireScope("billing:write")).Put("/settings", handlers.Billing.UpdateSettings)
					pr.With(middleware.RequireScope("billing:read")).Get("/bot-replies", handlers.Billing.GetBotReplies)
					pr.With(middleware.RequireScope("billing:read")).Get("/broadcasts", handlers.Billing.ListBroadcasts)
					pr.With(middleware.RequireScope("billing:write")).Post("/broadcasts", handlers.Billing.CreateBroadcast)
					pr.With(middleware.RequireScope("billing:read")).Get("/broadcasts/{id}", handlers.Billing.GetBroadcast)
				})

			})
		}

		if handlers.Credential != nil {
			apiRouter.Route("/credentials", func(cr chi.Router) {
				cr.Use(middleware.RequireAuth)
				cr.With(middleware.RequireScope("user:read")).Get("/", handlers.Credential.List)
				cr.With(middleware.RequireScope("user:write")).Post("/", handlers.Credential.Create)
				cr.With(middleware.RequireScope("user:read")).Get("/{id}", handlers.Credential.Get)
				cr.With(middleware.RequireScope("user:write")).Delete("/{id}", handlers.Credential.Delete)
				cr.With(middleware.RequireScope("user:write")).Post("/{id}/rotate", handlers.Credential.Rotate)
			})
		}

		if handlers.Analytics != nil {
			apiRouter.Route("/analytics", func(ar chi.Router) {
				ar.Use(middleware.RequireAuth)
				ar.With(middleware.RequireScope("user:read")).Get("/overview", handlers.Analytics.Overview)
				ar.With(middleware.RequireScope("user:read")).Get("/nodes", handlers.Analytics.GetByNode)
				ar.With(middleware.RequireScope("user:read")).Get("/users/{id}", handlers.Analytics.GetByUser)
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
				ar.Use(middleware.RequireScope("admin"))
				ar.Get("/", handlers.Admin.ListAdmins)
				ar.Post("/", handlers.Admin.CreateAdmin)
			})
			apiRouter.Route("/api-keys", func(kr chi.Router) {
				kr.Use(middleware.RequireAuth)
				kr.Use(middleware.RequireScope("admin"))
				kr.Get("/", handlers.Admin.ListAPIKeys)
				kr.Post("/", handlers.Admin.CreateAPIKey)
				kr.Delete("/{id}", handlers.Admin.DeleteAPIKey)
			})
		}

		if handlers.AI != nil {
			apiRouter.Route("/ai", func(air chi.Router) {
				air.Use(middleware.RequireAuth)
				// API keys carrying only the "ai" scope reach exclusively these endpoints:
				// every other resource group requires a different scope.
				air.Use(middleware.RequireScope("ai"))
				air.Post("/chat", handlers.AI.Chat)
				// Token-in-body variant: keeps the HMAC out of URLs (no path/query logging).
				// The legacy /actions/{token}/execute path variant is kept for compatibility.
				air.Post("/actions/execute", handlers.AI.ExecuteAction)
				air.Post("/actions/{token}/execute", handlers.AI.ExecuteAction)
				air.Get("/settings", handlers.AI.GetSettings)
				air.Post("/settings", handlers.AI.UpdateSettings)
				air.Post("/test", handlers.AI.TestConnection)
			})
		}
	})

	return r
}
