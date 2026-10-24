package migration

import "testing"

func TestCanTransition(t *testing.T) {
	all := []Status{StatusDraft, StatusRunning, StatusSuccess, StatusFailed}
	allowed := map[[2]Status]bool{
		{StatusDraft, StatusRunning}:   true,
		{StatusRunning, StatusSuccess}: true,
		{StatusRunning, StatusFailed}:  true,
	}
	for _, from := range all {
		for _, to := range all {
			if got := CanTransition(from, to); got != allowed[[2]Status{from, to}] {
				t.Errorf("CanTransition(%s, %s) = %v", from, to, got)
			}
		}
	}
}

func TestStatusHelpers(t *testing.T) {
	if !StatusDraft.Editable() || StatusRunning.Editable() || StatusSuccess.Editable() || StatusFailed.Editable() {
		t.Error("hanya draft yang boleh diedit")
	}
	if StatusDraft.Final() || StatusRunning.Final() || !StatusSuccess.Final() || !StatusFailed.Final() {
		t.Error("Final() salah")
	}
	if Status("aneh").Valid() || !StatusDraft.Valid() {
		t.Error("Valid() salah")
	}
}
