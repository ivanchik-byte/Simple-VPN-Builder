package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

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

				response.RespondInternalError(w, r, "Internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
