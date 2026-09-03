package response

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const ContentTypeProblemJSON = "application/problem+json"

// ProblemDetails represents an RFC 7807 Problem Details response.
type ProblemDetails struct {
	Type          string            `json:"type"`
	Title         string            `json:"title"`
	Status        int               `json:"status"`
	Detail        string            `json:"detail,omitempty"`
	Instance      string            `json:"instance,omitempty"`
	InvalidParams map[string]string `json:"invalid_params,omitempty"`
}

// RespondProblem writes an RFC 7807 problem response to the client.
func RespondProblem(w http.ResponseWriter, r *http.Request, status int, title, detail string, invalidParams map[string]string) {
	instance := ""
	if r != nil {
		instance = r.URL.Path
	}

	problem := ProblemDetails{
		Type:          fmt.Sprintf("https://httpstatuses.com/%d", status),
		Title:         title,
		Status:        status,
		Detail:        detail,
		Instance:      instance,
		InvalidParams: invalidParams,
	}

	w.Header().Set("Content-Type", ContentTypeProblemJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}

func RespondBadRequest(w http.ResponseWriter, r *http.Request, detail string, invalidParams map[string]string) {
	RespondProblem(w, r, http.StatusBadRequest, "Bad Request", detail, invalidParams)
}

func RespondUnauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	RespondProblem(w, r, http.StatusUnauthorized, "Unauthorized", detail, nil)
}

func RespondForbidden(w http.ResponseWriter, r *http.Request, detail string) {
	RespondProblem(w, r, http.StatusForbidden, "Forbidden", detail, nil)
}

func RespondNotFound(w http.ResponseWriter, r *http.Request, detail string) {
	RespondProblem(w, r, http.StatusNotFound, "Not Found", detail, nil)
}

func RespondConflict(w http.ResponseWriter, r *http.Request, detail string) {
	RespondProblem(w, r, http.StatusConflict, "Conflict", detail, nil)
}

func RespondRateLimited(w http.ResponseWriter, r *http.Request, retryAfterSec int) {
	if retryAfterSec > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSec))
	}
	RespondProblem(w, r, http.StatusTooManyRequests, "Too Many Requests", "Quota exceeded, please retry later", nil)
}

func RespondInternalError(w http.ResponseWriter, r *http.Request, detail string) {
	if detail == "" {
		detail = "An internal server error occurred"
	}
	RespondProblem(w, r, http.StatusInternalServerError, "Internal Server Error", detail, nil)
}
