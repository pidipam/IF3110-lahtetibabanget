package viewerapi

import (
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/explorer"
)

func TestToPagination(t *testing.T) {
	in := explorer.Pagination{Page: 2, PageSize: 10, Total: 25, TotalPages: 3, From: 11, To: 20, HasPrev: true, HasNext: true, PrevPage: 1, NextPage: 3}
	got := toPagination(in)
	if got.Page != 2 || got.PageSize != 10 || got.Total != 25 || got.TotalPages != 3 || got.From != 11 || got.To != 20 || !got.HasPrev || !got.HasNext || got.PrevPage != 1 || got.NextPage != 3 {
		t.Fatalf("paginasi: %+v", got)
	}
}

func TestToTableMeta(t *testing.T) {
	table := &explorer.Table{
		Schema: "public",
		Name:   "orders",
		Kind:   explorer.KindPartitioned,
		Columns: []explorer.Column{{
			Position: 1, Name: "id", Type: "integer", Nullable: false, HasDefault: true, Default: "nextval('orders_id_seq')",
		}},
		Constraints: []explorer.Constraint{{Name: "orders_pkey", Type: explorer.PrimaryKey, Definition: "PRIMARY KEY (id)", Columns: []string{"id"}}},
		Indexes:     []explorer.Index{{Name: "orders_pkey", Primary: true, Definition: "CREATE INDEX"}},
	}
	got := toTableMeta(table)
	if got.Schema != "public" || got.Name != "orders" || got.Kind != "partitioned" || got.KindLabel != "Partitioned table" {
		t.Fatalf("tabel: %+v", got)
	}
	if len(got.Columns) != 1 || got.Columns[0].Default != "nextval('orders_id_seq')" || !got.Columns[0].HasDefault {
		t.Fatalf("kolom: %+v", got.Columns)
	}
	if len(got.Constraints) != 1 || got.Constraints[0].Type != "PRIMARY KEY" || len(got.Constraints[0].Columns) != 1 {
		t.Fatalf("constraint: %+v", got.Constraints)
	}
	if len(got.Indexes) != 1 || !got.Indexes[0].Primary {
		t.Fatalf("index: %+v", got.Indexes)
	}
}
