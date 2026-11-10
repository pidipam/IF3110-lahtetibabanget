package approval

import (
	"strings"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
)

type State string

const (
	StateWaiting State = "waiting"
	StateBlocked State = "blocked"
	StateApproved State = "approved"
	StateRejected State = "rejected"
	StateStale State = "stale"
)

type Decision string

const (
	DecisionPending Decision = "pending"
	DecisionApproved Decision = "approved"
	DecisionRejected Decision = "rejected"
)

type Execution struct {
	ID int64
	MigrationID int64
	DatabaseID int64
	Title string
	DatabaseName string
	RevisionNumber int
	CurrentRevision int
	Kind string
	Status string
	RequestedBy int64
	RequestedByName string
	RejectReason string
	CreatedAt time.Time
}

type Group struct {
	ID int64
	Name string
	Level authz.Level
	LeaderID int64
	LeaderName string
	LeaderActive bool
}

type Vote struct {
	GroupID int64
	RevisionNumber int
	ApproverID int64
	ApproverName string
	Decision Decision
	Reason string
	DecidedAt time.Time
}

type GroupStatus struct {
	Group
	Decision Decision
	Vote *Vote
}

type Status struct {
	Execution Execution
	State State
	Message string
	Groups []GroupStatus
}

func (g Group) HasLeader() bool {
	return g.LeaderID != 0 && g.LeaderActive
}

func (e Execution) RevisionCurrent() bool {
	return e.Kind == "rollback" || e.RevisionNumber == e.CurrentRevision
}

func names(groups []GroupStatus) string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Name)
	}
	return strings.Join(out, ", ")
}

func Evaluate(exec Execution, groups []Group, votes []Vote) Status {
	st := Status{Execution: exec, Groups: make([]GroupStatus, 0, len(groups))}
	valid := map[int64]Vote{}
	for _, v := range votes {
		if v.RevisionNumber == exec.RevisionNumber {
			valid[v.GroupID] = v
		}
	}
	var pending, noLeader, selfLed, rejected []GroupStatus
	for _, g := range groups {
		gs := GroupStatus{Group: g, Decision: DecisionPending}
		if v, ok := valid[g.ID]; ok {
			gs.Decision = v.Decision
			gs.Vote = &v
		}
		switch {
		case gs.Decision == DecisionRejected:
			rejected = append(rejected, gs)
		case gs.Decision == DecisionPending && !g.HasLeader():
			noLeader = append(noLeader, gs)
		case gs.Decision == DecisionPending && g.LeaderID == exec.RequestedBy:
			selfLed = append(selfLed, gs)
		case gs.Decision == DecisionPending:
			pending = append(pending, gs)
		}
		st.Groups = append(st.Groups, gs)
	}
	switch {
	case exec.Status == "rejected" || len(rejected) > 0:
		st.State = StateRejected
		st.Message = "Permintaan ditolak."
		if exec.RejectReason != "" {
			st.Message = "Permintaan ditolak: " + exec.RejectReason
		}
	case !exec.RevisionCurrent():
		st.State = StateStale
		st.Message = "Revisi migration berubah setelah permintaan diajukan. Persetujuan lama tidak berlaku, ajukan ulang."
	case len(groups) == 0:
		st.State = StateBlocked
		st.Message = "Belum ada Grup yang punya akses ke database ini, jadi tidak ada yang bisa menyetujui."
	case len(noLeader) > 0 || len(selfLed) > 0:
		st.State = StateBlocked
		var parts []string
		if len(noLeader) > 0 {
			parts = append(parts, "Grup "+names(noLeader)+" belum punya leader aktif. Minta admin menetapkan leader.")
		}
		if len(selfLed) > 0 {
			parts = append(parts, "Pengaju adalah leader Grup "+names(selfLed)+" dan tidak boleh menyetujui sendiri. Minta admin menetapkan leader lain.")
		}
		st.Message = strings.Join(parts, " ")
	case len(pending) > 0:
		st.State = StateWaiting
		st.Message = "Menunggu persetujuan leader Grup " + names(pending) + "."
	default:
		st.State = StateApproved
		st.Message = "Semua leader Grup sudah menyetujui."
	}
	return st
}
