package adminapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
)

func TestUsernameRules(t *testing.T) {
	if normalizeUsername("  Carol.D ") != "carol.d" {
		t.Fatal("normalisasi username salah")
	}
	for _, s := range []string{"abc", "a_b-c.d", strings.Repeat("a", 32)} {
		if !validUsername(s) {
			t.Fatalf("%q harus valid", s)
		}
	}
	for _, s := range []string{"", "ab", "Abc", "a b c", "abc!", strings.Repeat("a", 33)} {
		if validUsername(s) {
			t.Fatalf("%q harus ditolak", s)
		}
	}
}

func TestPasswordProblem(t *testing.T) {
	if passwordProblem("password123") != "" {
		t.Fatal("password valid ditolak")
	}
	if passwordProblem("pendek") == "" || passwordProblem(strings.Repeat("x", 73)) == "" {
		t.Fatal("password tidak valid lolos")
	}
}

func TestGroupProblems(t *testing.T) {
	if len(groupProblems("tim-data", "Analis")) != 0 {
		t.Fatal("grup valid ditolak")
	}
	if len(groupProblems(strings.Repeat("é", MaxGroupName), strings.Repeat("é", MaxGroupDescription))) != 0 {
		t.Fatal("batas panjang dihitung per karakter")
	}
	d := groupProblems("", strings.Repeat("x", MaxGroupDescription+1))
	if d["name"] == "" || d["description"] == "" {
		t.Fatalf("detail kurang: %v", d)
	}
	if groupProblems(strings.Repeat("x", MaxGroupName+1), "")["name"] == "" {
		t.Fatal("nama terlalu panjang lolos")
	}
}

func TestUserEffect(t *testing.T) {
	cases := []struct {
		name string
		wasAdmin, wasActive, isAdmin, isActive bool
		want sessionEffect
	}{
		{"tidak berubah", false, true, false, true, effectNone},
		{"jadi admin", false, true, true, true, effectRotate},
		{"admin dicabut", true, true, false, true, effectRevoke},
		{"dinonaktifkan", false, true, false, false, effectRevoke},
		{"jadi admin tapi nonaktif", false, true, true, false, effectRevoke},
		{"diaktifkan kembali", false, false, false, true, effectNone},
	}
	for _, tc := range cases {
		if got := userEffect(tc.wasAdmin, tc.wasActive, tc.isAdmin, tc.isActive); got != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestValidateAccess(t *testing.T) {
	known := map[int64]bool{1: true, 2: true}
	changes, details := validateAccess([]accessItem{{DBID: 1, Level: "edit"}, {DBID: 2, Level: "none"}}, known)
	if details != nil || len(changes) != 2 || changes[0].Level != authz.LevelEdit || changes[1].Level != authz.LevelNone {
		t.Fatalf("got %v %v", changes, details)
	}
	cases := map[string][]accessItem{
		"access": nil,
		"access[0].db_id": {{DBID: 9, Level: "view"}},
		"access[1].db_id": {{DBID: 1, Level: "view"}, {DBID: 1, Level: "edit"}},
		"access[0].level": {{DBID: 1, Level: "admin"}},
	}
	for key, items := range cases {
		changes, details := validateAccess(items, known)
		if changes != nil || details[key] == "" {
			t.Fatalf("%s: got %v %v", key, changes, details)
		}
	}
}

func TestParseLeader(t *testing.T) {
	cases := map[string]struct {
		id int64
		ok bool
	}{"": {0, false}, "null": {0, true}, "5": {5, true}, "0": {0, false}, "-2": {0, false}, `"5"`: {0, false}, "1.5": {0, false}}
	for raw, want := range cases {
		id, ok := parseLeader(json.RawMessage(raw))
		if id != want.id || ok != want.ok {
			t.Fatalf("%q: got %d %v", raw, id, ok)
		}
	}
}
