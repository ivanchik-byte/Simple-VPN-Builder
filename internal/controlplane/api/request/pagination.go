package request

import (
	"net/http"
	"strconv"
)

type PaginationParams struct {
	Page    int
	PerPage int
	Limit   int
	Offset  int
}

func ParsePagination(r *http.Request) PaginationParams {
	page := 1
	perPage := 20

	if pStr := r.URL.Query().Get("page"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			page = p
		}
	}

	if ppStr := r.URL.Query().Get("per_page"); ppStr != "" {
		if pp, err := strconv.Atoi(ppStr); err == nil && pp > 0 {
			if pp > 100 {
				pp = 100
			}
			perPage = pp
		}
	}

	offset := (page - 1) * perPage
	return PaginationParams{
		Page:    page,
		PerPage: perPage,
		Limit:   perPage,
		Offset:  offset,
	}
}

type PaginationMeta struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	TotalItems int64 `json:"total_items"`
	TotalPages int   `json:"total_pages"`
}

type PaginatedResponse[T any] struct {
	Items      []T            `json:"items"`
	Pagination PaginationMeta `json:"pagination"`
}

func NewPaginatedResponse[T any](items []T, params PaginationParams, totalItems int64) PaginatedResponse[T] {
	if items == nil {
		items = []T{}
	}
	totalPages := 0
	if params.PerPage > 0 && totalItems > 0 {
		totalPages = int((totalItems + int64(params.PerPage) - 1) / int64(params.PerPage))
	}

	return PaginatedResponse[T]{
		Items: items,
		Pagination: PaginationMeta{
			Page:       params.Page,
			PerPage:    params.PerPage,
			TotalItems: totalItems,
			TotalPages: totalPages,
		},
	}
}
