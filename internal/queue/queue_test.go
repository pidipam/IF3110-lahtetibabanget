package queue

import (
	"regexp"
	"testing"
)

func TestNewJobIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		id, err := newJobID()
		if err != nil {
			t.Fatal(err)
		}
		if !re.MatchString(id) {
			t.Fatalf("format UUID tidak valid: %q", id)
		}
		if seen[id] {
			t.Fatalf("job_id duplikat: %q", id)
		}
		seen[id] = true
	}
}
