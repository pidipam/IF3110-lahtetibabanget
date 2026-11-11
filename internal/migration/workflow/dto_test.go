package workflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDTOJSONShape(t *testing.T) {
	ord := 2
	r := MigrationResponse{
		ID: 7, DatabaseID: 3, Title: "x", LinearOrder: &ord, CurrentRevision: 1,
		CreatedAt: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC),
		CanEdit:   true,
	}
	b, _ := json.Marshal(r)
	s := string(b)
	for _, k := range []string{`"id":7`, `"database_id":3`, `"linear_order":2`, `"latest_execution":null`,
		`"can_edit":true`, `"can_rollback":false`, `"created_at":"2026-11-07T10:00:00Z"`} {
		if !strings.Contains(s, k) {
			t.Errorf("hilang %s dalam %s", k, s)
		}
	}
	e, _ := json.Marshal(ToExecutionResponse(Execution{ID: 1, Kind: KindRollback, Status: StatusPartiallyReverted}))
	if !strings.Contains(string(e), `"status":"partially_reverted"`) || !strings.Contains(string(e), `"undoes_execution_id":null`) {
		t.Errorf("execution json: %s", e)
	}
	var req CreateRequest
	if err := json.Unmarshal([]byte(`{"title":"a","sql_up":"x","is_batch_mode":true,"batch_column":"id","batch_size":5}`), &req); err != nil || req.BatchSize != 5 {
		t.Errorf("decode: %v %+v", err, req)
	}
}
