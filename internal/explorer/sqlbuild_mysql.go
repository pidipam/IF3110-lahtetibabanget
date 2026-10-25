package explorer

import "strings"

func QuoteIdentMySQL(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

func QuoteQualifiedMySQL(parts ...string) string {
	q := make([]string, len(parts))
	for i, p := range parts {
		q[i] = QuoteIdentMySQL(p)
	}
	return strings.Join(q, ".")
}

func ChooseOrderingMySQL(t *Table) Ordering {
	if len(t.PrimaryKey()) > 0 {
		return OrderPK
	}
	return OrderNone
}

func BuildCountMySQL(t *Table) string {
	return "SELECT COUNT(*) FROM " + QuoteQualifiedMySQL(t.Schema, t.Name)
}

func BuildSelectMySQL(t *Table, ord Ordering) (string, bool) {
	if len(t.Columns) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("SELECT ")
	for i, c := range t.Columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("LEFT(CAST(t." + QuoteIdentMySQL(c.Name) + " AS CHAR), " + itoa(MaxCellChars+1) + ") AS " + QuoteIdentMySQL(c.Name))
	}
	b.WriteString(" FROM " + QuoteQualifiedMySQL(t.Schema, t.Name) + " AS t")
	if ord == OrderPK {
		b.WriteString(" ORDER BY ")
		for i, c := range t.PrimaryKey() {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("t." + QuoteIdentMySQL(c))
		}
	}
	b.WriteString(" LIMIT ? OFFSET ?")
	return b.String(), true
}
