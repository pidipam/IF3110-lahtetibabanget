package approval

import (
	"errors"
	"strings"
	"testing"
)

func exec(kind string, rev, current int) Execution {
	return Execution{ID: 1, Kind: kind, Status: "pending_review", RevisionNumber: rev, CurrentRevision: current}
}

func led(id int64, name string) Group {
	return Group{ID: id, Name: name, LeaderID: 100 + id, LeaderName: "leader", LeaderActive: true}
}

func vote(group int64, rev int, d Decision) Vote {
	return Vote{GroupID: group, RevisionNumber: rev, ApproverID: 100 + group, Decision: d}
}

func TestEvaluate(t *testing.T) {
	inactive := led(3, "Gudang")
	inactive.LeaderActive = false
	cases := []struct {
		name string
		exec Execution
		groups []Group
		votes []Vote
		state State
		message string
	}{
		{"satu grup menunggu", exec("up", 2, 2), []Group{led(1, "Analis")}, nil, StateWaiting, "grup Analis"},
		{"satu grup setuju", exec("up", 2, 2), []Group{led(1, "Analis")}, []Vote{vote(1, 2, DecisionApproved)}, StateApproved, "Semua leader"},
		{"banyak grup sebagian", exec("up", 1, 1), []Group{led(1, "Analis"), led(2, "Ops")}, []Vote{vote(1, 1, DecisionApproved)}, StateWaiting, "grup Ops."},
		{"banyak grup semua", exec("up", 1, 1), []Group{led(1, "Analis"), led(2, "Ops")}, []Vote{vote(1, 1, DecisionApproved), vote(2, 1, DecisionApproved)}, StateApproved, ""},
		{"grup tanpa leader", exec("up", 1, 1), []Group{led(1, "Analis"), {ID: 2, Name: "Ops"}}, []Vote{vote(1, 1, DecisionApproved)}, StateBlocked, "Grup Ops belum punya leader aktif"},
		{"leader nonaktif", exec("up", 1, 1), []Group{inactive}, nil, StateBlocked, "Grup Gudang belum punya leader aktif"},
		{"pengaju memimpin grup wajib", Execution{Kind: "up", Status: "pending_review", RevisionNumber: 1, CurrentRevision: 1, RequestedBy: 101}, []Group{led(1, "Analis"), led(2, "Ops")}, []Vote{vote(2, 1, DecisionApproved)}, StateBlocked, "Pengaju adalah leader grup Analis"},
		{"tanpa leader dan pengaju leader", Execution{Kind: "up", Status: "pending_review", RevisionNumber: 1, CurrentRevision: 1, RequestedBy: 101}, []Group{led(1, "Analis"), {ID: 2, Name: "Ops"}}, nil, StateBlocked, "Grup Ops belum punya leader aktif. Minta admin menetapkan leader. Pengaju adalah leader grup Analis"},
		{"tanpa grup", exec("up", 1, 1), nil, nil, StateBlocked, "Belum ada grup"},
		{"satu menolak", exec("up", 1, 1), []Group{led(1, "Analis"), led(2, "Ops")}, []Vote{vote(1, 1, DecisionApproved), vote(2, 1, DecisionRejected)}, StateRejected, "ditolak"},
		{"revisi lama tidak dihitung", exec("up", 3, 3), []Group{led(1, "Analis")}, []Vote{vote(1, 2, DecisionApproved)}, StateWaiting, ""},
		{"revisi berubah", exec("up", 2, 3), []Group{led(1, "Analis")}, []Vote{vote(1, 2, DecisionApproved)}, StateStale, "Revisi migration berubah"},
		{"forward ikut revisi terkini", exec("forward", 2, 3), []Group{led(1, "Analis")}, nil, StateStale, ""},
		{"rollback revisi lama tetap sah", exec("rollback", 1, 3), []Group{led(1, "Analis")}, []Vote{vote(1, 1, DecisionApproved)}, StateApproved, ""},
		{"grup sudah setuju lalu leader dilepas", exec("up", 1, 1), []Group{{ID: 1, Name: "Analis"}}, []Vote{vote(1, 1, DecisionApproved)}, StateApproved, ""},
	}
	for _, tc := range cases {
		st := Evaluate(tc.exec, tc.groups, tc.votes)
		if st.State != tc.state || !strings.Contains(st.Message, tc.message) {
			t.Fatalf("%s: got %s %q", tc.name, st.State, st.Message)
		}
		if len(st.Groups) != len(tc.groups) {
			t.Fatalf("%s: jumlah grup %d", tc.name, len(st.Groups))
		}
	}
}

func TestEvaluateRejectedExecutionKeepsReason(t *testing.T) {
	e := exec("up", 1, 1)
	e.Status = "rejected"
	e.RejectReason = "Index belum diuji."
	st := Evaluate(e, []Group{led(1, "Analis")}, []Vote{vote(1, 1, DecisionRejected)})
	if st.State != StateRejected || st.Message != "Permintaan ditolak: Index belum diuji." {
		t.Fatalf("got %s %q", st.State, st.Message)
	}
	if st.Groups[0].Vote == nil || st.Groups[0].Decision != DecisionRejected {
		t.Fatal("keputusan grup harus terlihat")
	}
}

func TestStaleErrorIs(t *testing.T) {
	err := error(&StaleError{State: StateWaiting, Message: "Menunggu persetujuan leader grup Ops."})
	if !errors.Is(err, ErrApprovalStale) || !strings.Contains(err.Error(), "grup Ops") {
		t.Fatalf("got %v", err)
	}
}
