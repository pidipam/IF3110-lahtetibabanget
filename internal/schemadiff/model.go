package schemadiff

type Schema struct {
	Name      string
	Tables    []Table
	Views     []View
	Enums     []Enum
	Sequences []Sequence
}

type Table struct {
	Name        string
	Columns     []Column
	Constraints []Constraint
	Indexes     []Index
}

type Column struct {
	Name       string
	Type       string  
	NotNull    bool
	HasDefault bool
	Default    string 
	Identity   string 
	Generated  string 
	Expression string 
}

const (
	ConPrimary   = "p"
	ConUnique    = "u"
	ConForeign   = "f"
	ConCheck     = "c"
	ConExclusion = "x"
)

type Constraint struct {
	Name       string
	Type       string
	Definition string 
}

type Index struct {
	Name   string
	Unique bool
	Body   string 
}

type View struct {
	Name       string
	Definition string 
}

type Enum struct {
	Name   string
	Labels []string
}

type Sequence struct {
	Name        string
	OwnerTable  string 
	OwnerColumn string
}

func (s *Schema) table(name string) *Table {
	for i := range s.Tables {
		if s.Tables[i].Name == name {
			return &s.Tables[i]
		}
	}
	return nil
}

func (s *Schema) view(name string) *View {
	for i := range s.Views {
		if s.Views[i].Name == name {
			return &s.Views[i]
		}
	}
	return nil
}

func (s *Schema) enum(name string) *Enum {
	for i := range s.Enums {
		if s.Enums[i].Name == name {
			return &s.Enums[i]
		}
	}
	return nil
}

func (s *Schema) sequence(name string) *Sequence {
	for i := range s.Sequences {
		if s.Sequences[i].Name == name {
			return &s.Sequences[i]
		}
	}
	return nil
}

func (t *Table) column(name string) *Column {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}

func (t *Table) constraint(name string) *Constraint {
	for i := range t.Constraints {
		if t.Constraints[i].Name == name {
			return &t.Constraints[i]
		}
	}
	return nil
}

func (t *Table) index(name string) *Index {
	for i := range t.Indexes {
		if t.Indexes[i].Name == name {
			return &t.Indexes[i]
		}
	}
	return nil
}
