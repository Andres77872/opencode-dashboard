package stats

import (
	"testing"
)

func TestDefaultPagination(t *testing.T) {
	p := DefaultPagination()

	if p.Page != 1 {
		t.Errorf("DefaultPagination().Page = %d, want 1", p.Page)
	}
	if p.PageSize != 20 {
		t.Errorf("DefaultPagination().PageSize = %d, want 20", p.PageSize)
	}
}

func TestPaginationOffset(t *testing.T) {
	tests := []struct {
		name     string
		page     int
		pageSize int
		expected int
	}{
		{name: "page 1", page: 1, pageSize: 20, expected: 0},
		{name: "page 2", page: 2, pageSize: 20, expected: 20},
		{name: "page 3", page: 3, pageSize: 20, expected: 40},
		{name: "page 5 size 10", page: 5, pageSize: 10, expected: 40},
		{name: "large page", page: 100, pageSize: 50, expected: 4950},
		{name: "page zero normalized", page: 0, pageSize: 20, expected: 0},
		{name: "negative page normalized", page: -5, pageSize: 20, expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Pagination{Page: tt.page, PageSize: tt.pageSize}
			result := p.Offset()

			if result != tt.expected {
				t.Errorf("Pagination{Page:%d, PageSize:%d}.Offset() = %d, want %d", tt.page, tt.pageSize, result, tt.expected)
			}
		})
	}
}
