package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrors_RFC7807Responses(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test/path", nil)

	t.Run("BadRequest", func(t *testing.T) {
		rec := httptest.NewRecorder()
		params := map[string]string{"username": "required"}
		RespondBadRequest(rec, req, "Invalid input data", params)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, ContentTypeProblemJSON, rec.Header().Get("Content-Type"))

		var problem ProblemDetails
		err := json.Unmarshal(rec.Body.Bytes(), &problem)
		require.NoError(t, err)
		assert.Equal(t, 400, problem.Status)
		assert.Equal(t, "Bad Request", problem.Title)
		assert.Equal(t, "Invalid input data", problem.Detail)
		assert.Equal(t, "/test/path", problem.Instance)
		assert.Equal(t, "required", problem.InvalidParams["username"])
	})

	t.Run("Unauthorized", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RespondUnauthorized(rec, req, "Token expired")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("Forbidden", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RespondForbidden(rec, req, "Insufficient permissions")
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("NotFound", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RespondNotFound(rec, req, "Node not found")
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("Conflict", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RespondConflict(rec, req, "User already exists")
		assert.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("RateLimited", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RespondRateLimited(rec, req, 60)
		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		assert.Equal(t, "60", rec.Header().Get("Retry-After"))
	})

	t.Run("InternalError", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RespondInternalError(rec, req, "")
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}
