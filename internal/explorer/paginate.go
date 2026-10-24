package explorer

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	DefaultPageSize = 25
	MinPageSize     = 1
	MaxPageSize     = 100
	MaxCellChars = 500 // karakter per sel
)

func ParsePageParams(pageStr, sizeStr string) (page, size int) {
	page, size = 1, DefaultPageSize
	if n, err := strconv.Atoi(strings.TrimSpace(pageStr)); err == nil && n >= 1 {
		page = n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(sizeStr)); err == nil {
		size = ClampPageSize(n)
	}
	return page, size
}

func ClampPageSize(n int) int {
	switch {
	case n < MinPageSize:
		return DefaultPageSize
	case n > MaxPageSize:
		return MaxPageSize
	}
	return n
}

type Pagination struct {
	Page       int
	PageSize   int
	Total      int64
	TotalPages int
	Offset     int64
	From       int64
	To         int64
	HasPrev    bool
	HasNext    bool
	PrevPage   int
	NextPage   int
}

func Paginate(total int64, page, size int) Pagination {
	size = ClampPageSize(size)
	if total < 0 {
		total = 0
	}
	totalPages := int((total + int64(size) - 1) / int64(size))
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	p := Pagination{Page: page, PageSize: size, Total: total, TotalPages: totalPages}
	p.Offset = int64(page-1) * int64(size)
	if total > 0 {
		p.From = p.Offset + 1
		p.To = p.Offset + int64(size)
		if p.To > total {
			p.To = total
		}
	}
	p.HasPrev = page > 1
	p.HasNext = page < totalPages
	p.PrevPage = page - 1
	p.NextPage = page + 1
	return p
}

type Cell struct {
	Text      string
	Null      bool
	Truncated bool
}

func NewCell(v *string) Cell {
	if v == nil {
		return Cell{Null: true}
	}
	s := *v
	if utf8.RuneCountInString(s) <= MaxCellChars {
		return Cell{Text: s}
	}
	runes := []rune(s)
	return Cell{Text: string(runes[:MaxCellChars]), Truncated: true}
}
