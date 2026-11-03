package api

import (
	"net/http/httptest"
	"testing"
)

func TestParsePageQueryAppliesDefaults(t *testing.T) {
	cases := []struct {
		name       string
		rawQuery   string
		wantPage   int
		wantSize   int
		wantOffset int
	}{
		{"absent", "", 1, DefaultPageSize, 0},
		{"explicit", "page=3&page_size=25", 3, 25, 50},
		{"page_below_one", "page=0", 1, DefaultPageSize, 0},
		{"page_negative", "page=-2", 1, DefaultPageSize, 0},
		{"page_garbage", "page=abc", 1, DefaultPageSize, 0},
		{"size_below_one", "page_size=0", 1, DefaultPageSize, 0},
		{"size_above_cap", "page_size=5000", 1, MaxPageSize, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/items?"+tc.rawQuery, nil)
			got := parsePageQuery(r)
			if got.Page != tc.wantPage || got.PageSize != tc.wantSize {
				t.Fatalf("query %q got page=%d size=%d, want page=%d size=%d",
					tc.rawQuery, got.Page, got.PageSize, tc.wantPage, tc.wantSize)
			}
			if offset := got.Offset(); offset != tc.wantOffset {
				t.Fatalf("query %q offset=%d, want %d", tc.rawQuery, offset, tc.wantOffset)
			}
		})
	}
}
