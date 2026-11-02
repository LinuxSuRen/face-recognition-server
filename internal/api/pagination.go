package api

import (
	"net/http"
	"strconv"
)

// DefaultPageSize is used when a list request omits page_size.
const DefaultPageSize = 10

// MaxPageSize caps page_size to keep responses bounded.
const MaxPageSize = 100

// PageQuery is the canonical pagination contract: `page` starts at 1 and
// `page_size` is bounded by MaxPageSize.
type PageQuery struct {
	Page     int
	PageSize int
}

// Offset is the SQL offset for this query.
func (q PageQuery) Offset() int {
	return (q.Page - 1) * q.PageSize
}

// PageResponse is the canonical list envelope shared by every list endpoint.
type PageResponse struct {
	Data     any `json:"data"`
	Page     int `json:"page"`
	Total    int `json:"total"`
	PageSize int `json:"page_size"`
}

// parsePageQuery reads page/page_size from the request with boundary checks:
// page < 1 becomes 1 and page_size outside (0, MaxPageSize] falls back to the
// default.
func parsePageQuery(r *http.Request) PageQuery {
	query := r.URL.Query()
	page, err := strconv.Atoi(query.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(query.Get("page_size"))
	if err != nil || pageSize < 1 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return PageQuery{Page: page, PageSize: pageSize}
}
