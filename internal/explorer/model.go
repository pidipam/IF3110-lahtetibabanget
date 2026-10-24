package explorer

import (
	"sort"
	"strings"
)

type Kind string

const (
	KindTable       Kind = "table"
	KindPartitioned Kind = "partitioned"
	KindView        Kind = "view"
	KindMatView     Kind = "matview"
	KindForeign     Kind = "foreign"
)

func kindFromRelkind(r string) Kind {
	switch r {
	case "p":
		return KindPartitioned
	case "v":
		return KindView
	case "m":
		return KindMatView
	case "f":
		return KindForeign
	}
	return KindTable
}

func (k Kind) Label() string {
	switch k {
	case KindPartitioned:
		return "Partitioned table"
	case KindView:
		return "View"
	case KindMatView:
		return "Materialized view"
	case KindForeign:
		return "Foreign table"
	}
	return "Table"
}

type ConstraintType string

const (
	PrimaryKey ConstraintType = "PRIMARY KEY"
	Unique     ConstraintType = "UNIQUE"
	ForeignKey ConstraintType = "FOREIGN KEY"
	Check      ConstraintType = "CHECK"
)

func constraintTypeFrom(c string) (ConstraintType, bool) {
	switch c {
	case "p":
		return PrimaryKey, true
	case "u":
		return Unique, true
	case "f":
		return ForeignKey, true
	case "c":
		return Check, true
	}
	return "", false
}

func (t ConstraintType) rank() int {
	switch t {
	case PrimaryKey:
		return 0
	case Unique:
		return 1
	case ForeignKey:
		return 2
	}
	return 3
}

type Column struct {
	Position   int
	Name       string
	Type       string
	Nullable   bool
	HasDefault bool
	Default    string
}

type Constraint struct {
	Name       string
	Type       ConstraintType
	Definition string
	Columns    []string
}

type Index struct {
	Name       string
	Unique     bool
	Primary    bool
	Definition string
}

type Table struct {
	Schema      string
	Name        string
	Kind        Kind
	Columns     []Column
	Constraints []Constraint
	Indexes     []Index
}

// FullName untuk tampilan saja (tidak di-quote). Jangan dipakai untuk membangun SQL.
func (t *Table) FullName() string { return t.Schema + "." + t.Name }

// PrimaryKey mengembalikan nama kolom primary key berurutan, atau nil bila tidak ada.
func (t *Table) PrimaryKey() []string {
	for _, c := range t.Constraints {
		if c.Type == PrimaryKey {
			return c.Columns
		}
	}
	return nil
}

type Schema struct {
	Name   string
	Tables []*Table
}

type Catalog struct {
	Schemas []*Schema
}

// Table adalah whitelist: hanya tabel yang benar-benar ada di hasil introspeksi yang dikembalikan.
func (c *Catalog) Table(schema, name string) (*Table, bool) {
	for _, s := range c.Schemas {
		if s.Name != schema {
			continue
		}
		for _, t := range s.Tables {
			if t.Name == name {
				return t, true
			}
		}
	}
	return nil, false
}

func (c *Catalog) Schema(name string) (*Schema, bool) {
	for _, s := range c.Schemas {
		if s.Name == name {
			return s, true
		}
	}
	return nil, false
}

// Hasil mentah query introspeksi. Dipisah dari pgx agar perakitan bisa diuji.
type rawTable struct{ Schema, Name, Relkind string }

type rawColumn struct {
	Schema, Table, Name, Type string
	Position                  int
	Nullable                  bool
	Default                   *string
}

type rawConstraint struct {
	Schema, Table, Name, Contype, Def string
	Columns                           []string
}

type rawIndex struct {
	Schema, Table, Name, Def string
	Unique, Primary          bool
}

func key(schema, table string) string { return schema + "\x00" + table }

func assemble(schemas []string, tables []rawTable, cols []rawColumn, cons []rawConstraint, idxs []rawIndex) *Catalog {
	cat := &Catalog{}
	bySchema := map[string]*Schema{}
	for _, name := range schemas {
		s := &Schema{Name: name}
		bySchema[name] = s
		cat.Schemas = append(cat.Schemas, s)
	}
	byTable := map[string]*Table{}
	for _, rt := range tables {
		s, ok := bySchema[rt.Schema]
		if !ok {
			s = &Schema{Name: rt.Schema}
			bySchema[rt.Schema] = s
			cat.Schemas = append(cat.Schemas, s)
		}
		t := &Table{Schema: rt.Schema, Name: rt.Name, Kind: kindFromRelkind(rt.Relkind)}
		s.Tables = append(s.Tables, t)
		byTable[key(rt.Schema, rt.Name)] = t
	}
	for _, rc := range cols {
		if t, ok := byTable[key(rc.Schema, rc.Table)]; ok {
			c := Column{Position: rc.Position, Name: rc.Name, Type: rc.Type, Nullable: rc.Nullable}
			if rc.Default != nil {
				c.HasDefault, c.Default = true, *rc.Default
			}
			t.Columns = append(t.Columns, c)
		}
	}
	for _, rc := range cons {
		ct, ok := constraintTypeFrom(rc.Contype)
		if !ok {
			continue
		}
		if t, ok := byTable[key(rc.Schema, rc.Table)]; ok {
			t.Constraints = append(t.Constraints, Constraint{Name: rc.Name, Type: ct, Definition: rc.Def, Columns: rc.Columns})
		}
	}
	for _, ri := range idxs {
		if t, ok := byTable[key(ri.Schema, ri.Table)]; ok {
			t.Indexes = append(t.Indexes, Index{Name: ri.Name, Unique: ri.Unique, Primary: ri.Primary, Definition: ri.Def})
		}
	}
	sort.Slice(cat.Schemas, func(i, j int) bool { return cat.Schemas[i].Name < cat.Schemas[j].Name })
	for _, s := range cat.Schemas {
		sort.SliceStable(s.Tables, func(i, j int) bool { return strings.Compare(s.Tables[i].Name, s.Tables[j].Name) < 0 })
		for _, t := range s.Tables {
			sort.SliceStable(t.Columns, func(i, j int) bool { return t.Columns[i].Position < t.Columns[j].Position })
			sort.SliceStable(t.Constraints, func(i, j int) bool {
				a, b := t.Constraints[i], t.Constraints[j]
				if a.Type.rank() != b.Type.rank() {
					return a.Type.rank() < b.Type.rank()
				}
				return a.Name < b.Name
			})
			sort.SliceStable(t.Indexes, func(i, j int) bool {
				a, b := t.Indexes[i], t.Indexes[j]
				if a.Primary != b.Primary {
					return a.Primary
				}
				return a.Name < b.Name
			})
		}
	}
	return cat
}
