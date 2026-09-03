package request

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePagination(t *testing.T) {
	t.Run("DefaultValues", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		p := ParsePagination(req)
		assert.Equal(t, 1, p.Page)
		assert.Equal(t, 20, p.PerPage)
		assert.Equal(t, 20, p.Limit)
		assert.Equal(t, 0, p.Offset)
	})

	t.Run("CustomPageAndPerPage", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test?page=3&per_page=15", nil)
		p := ParsePagination(req)
		assert.Equal(t, 3, p.Page)
		assert.Equal(t, 15, p.PerPage)
		assert.Equal(t, 15, p.Limit)
		assert.Equal(t, 30, p.Offset)
	})

	t.Run("CapPerPageAt100", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test?page=1&per_page=500", nil)
		p := ParsePagination(req)
		assert.Equal(t, 100, p.PerPage)
		assert.Equal(t, 100, p.Limit)
	})
}

func TestNewPaginatedResponse(t *testing.T) {
	params := PaginationParams{Page: 2, PerPage: 10, Limit: 10, Offset: 10}
	items := []string{"item1", "item2"}

	resp := NewPaginatedResponse(items, params, 25)
	assert.Equal(t, items, resp.Items)
	assert.Equal(t, 2, resp.Pagination.Page)
	assert.Equal(t, 10, resp.Pagination.PerPage)
	assert.Equal(t, int64(25), resp.Pagination.TotalItems)
	assert.Equal(t, 3, resp.Pagination.TotalPages)
}
