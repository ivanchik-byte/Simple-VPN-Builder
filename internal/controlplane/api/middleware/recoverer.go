package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
)

// Recoverer catches panics, logs stack traces, and returns an RFC 7807 500 error.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				if rvr == http.ErrAbortHandler {
					panic(rvr)
				}

				stack := debug.Stack()
				logger.ErrorContext(r.Context(), "Panic recovered in HTTP handler",
					"error", fmt.Sprintf("%v", rvr),
					"stack", string(stack),
				)

				if strings.Contains(r.Header.Get("Accept"), "text/html") || strings.HasPrefix(r.URL.Path, "/admin/") {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="en" class="dark"><head><meta charset="utf-8"><title>500 — Internal Server Error</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{background:#09090b;color:#f4f4f6;font-family:Inter,-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;padding:1rem;}
.card{width:100%;max-width:28rem;border:1px solid rgba(255,255,255,0.08);border-radius:0.75rem;background:#121215;padding:2rem;box-shadow:0 20px 50px rgba(0,0,0,0.5);text-align:center;position:relative;overflow:hidden;}
.glow{position:absolute;top:0;left:0;right:0;height:6rem;background:radial-gradient(60% 100% at 50% 0%,rgba(244,63,94,0.14),transparent 70%);pointer-events:none;}
.code{display:inline-flex;align-items:center;justify-content:center;padding:0 1.25rem;height:3.5rem;border-radius:1rem;background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.08);margin-bottom:1rem;font-family:monospace;font-size:1.5rem;font-weight:700;color:#fb7185;}
h1{font-size:1.125rem;font-weight:600;margin:0 0 0.5rem;}
p{font-size:0.75rem;color:#71717a;font-family:monospace;margin:0 0 1.5rem;line-height:1.6;}
.row{display:flex;gap:0.75rem;justify-content:center;}
.btn{padding:0.5rem 1rem;border-radius:0.5rem;font-size:0.75rem;font-weight:500;text-transform:uppercase;letter-spacing:0.05em;text-decoration:none;}
.primary{background:#f4f4f6;color:#09090b;}
.ghost{border:1px solid rgba(255,255,255,0.14);color:#a1a1aa;background:rgba(255,255,255,0.04);cursor:pointer;font-family:monospace;}</style></head>
<body><div class="card"><div class="glow"></div><div style="position:relative">
<div class="code">500</div>
<h1>Internal Server Error</h1>
<p>Something went wrong on our side. The incident was logged — try again in a moment.</p>
<div class="row"><a class="btn primary" href="/admin/dashboard">Control Plane</a><button class="btn ghost" onclick="window.history.back()">Go Back</button></div>
</div></div></body></html>`))
					return
				}

				response.RespondInternalError(w, r, "Internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
