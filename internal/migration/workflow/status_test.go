package workflow

import "testing"

func TestCanTransition(t *testing.T) {
	ok := [][2]Status{
		{StatusPendingReview, StatusApproved}, {StatusPendingReview, StatusRejected},
		{StatusApproved, StatusQueued}, {StatusQueued, StatusRunning},
		{StatusRunning, StatusSuccess}, {StatusRunning, StatusFailed},
		{StatusRunning, StatusPartial}, {StatusFailed, StatusQueued},
		{StatusPartial, StatusRunning}, {StatusUncertain, StatusRunning},
		{StatusPartiallyReverted, StatusRunning},
	}
	for _, c := range ok {
		if err := CanTransition(c[0], c[1]); err != nil {
			t.Errorf("%s->%s harus boleh: %v", c[0], c[1], err)
		}
	}
	bad := [][2]Status{
		{StatusSuccess, StatusRunning}, {StatusRejected, StatusApproved},
		{StatusCancelled, StatusQueued}, {StatusPendingReview, StatusRunning},
		{StatusApproved, StatusSuccess}, {StatusQueued, StatusSuccess},
	}
	for _, c := range bad {
		if CanTransition(c[0], c[1]) == nil {
			t.Errorf("%s->%s harus ditolak", c[0], c[1])
		}
	}
}

func TestHoldsDatabase(t *testing.T) {
	for _, s := range []Status{StatusRunning, StatusPartial, StatusUncertain, StatusPartiallyReverted} {
		if !s.HoldsDatabase() {
			t.Errorf("%s harus menahan database", s)
		}
	}
	for _, s := range []Status{StatusQueued, StatusFailed, StatusSuccess, StatusApproved} {
		if s.HoldsDatabase() {
			t.Errorf("%s tidak boleh menahan database", s)
		}
	}
}

func TestExtraTransitions(t *testing.T) {
	for _, c := range [][2]Status{
		{StatusRunning, StatusQueued},            // retry sementara dengan jeda
		{StatusRunning, StatusCancelled},         // batch selesai dibatalkan penuh
		{StatusPartial, StatusPartiallyReverted}, // pengguna membatalkan batch
	} {
		if err := CanTransition(c[0], c[1]); err != nil {
			t.Errorf("%s->%s: %v", c[0], c[1], err)
		}
	}
}
