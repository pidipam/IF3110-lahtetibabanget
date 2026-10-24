package explorer

import "strings"

func QuoteIdent(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func QuoteQualified(parts ...string) string {
	q := make([]string, len(parts))
	for i, p := range parts {
		q[i] = QuoteIdent(p)
	}
	return strings.Join(q, ".")
}

type Ordering string

const (
	OrderPK   Ordering = "primary key"
	OrderCTID Ordering = "ctid"
	OrderNone Ordering = "none"
)

func ChooseOrdering(t *Table) Ordering {
	if len(t.PrimaryKey()) > 0 {
		return OrderPK
	}
	if t.Kind == KindTable || t.Kind == KindMatView {
		return OrderCTID
	}
	return OrderNone
}

func BuildCount(t *Table) string {
	return "SELECT count(*) FROM " + QuoteQualified(t.Schema, t.Name)
}

func BuildSelect(t *Table, ord Ordering) (string, bool) {
	if len(t.Columns) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("SELECT ")
	for i, c := range t.Columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("left(t.")
		b.WriteString(QuoteIdent(c.Name))
		b.WriteString("::text, ")
		b.WriteString(itoa(MaxCellChars + 1))
		b.WriteString(") AS ")
		b.WriteString(QuoteIdent(c.Name))
	}
	b.WriteString(" FROM ")
	b.WriteString(QuoteQualified(t.Schema, t.Name))
	b.WriteString(" AS t")
	switch ord {
	case OrderPK:
		pk := t.PrimaryKey()
		b.WriteString(" ORDER BY ")
		for i, c := range pk {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("t.")
			b.WriteString(QuoteIdent(c))
		}
	case OrderCTID:
		b.WriteString(" ORDER BY t.ctid")
	}
	b.WriteString(" LIMIT $1 OFFSET $2")
	return b.String(), true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
