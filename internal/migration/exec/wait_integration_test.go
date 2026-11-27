package exec

import (
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
)

func TestLaterMigrationWaitsForEarlierQueued(t *testing.T) {
	r := newRig(t)
	a := r.ready(r.mig("A", "CREATE TABLE zz_p(id int)", "DROP TABLE zz_p"))
	b := r.ready(r.mig("B", "CREATE TABLE zz_q(id int)", "DROP TABLE zz_q"))
	before := r.q.Count()

	r.run(b)
	if st := r.ex(b).Status; st != workflow.StatusQueued {
		t.Fatalf("B harus menunggu di antrean, bukan %s", st)
	}
	if r.histFailed() != 0 || r.hist() != 0 {
		t.Fatalf("menunggu giliran bukan attempt gagal: sukses=%d gagal=%d", r.hist(), r.histFailed())
	}
	if r.locks() != 0 {
		t.Fatal("lock database harus dilepas saat B menunggu")
	}
	if r.q.Count() != before+1 || r.q.Jobs[len(r.q.Jobs)-1].ExecID != b {
		t.Fatalf("B harus dijadwalkan ulang: %+v", r.q.Jobs[before:])
	}

	r.run(a)
	r.run(b)
	if r.ex(a).Status != workflow.StatusSuccess || r.ex(b).Status != workflow.StatusSuccess {
		t.Fatalf("A=%s B=%s", r.ex(a).Status, r.ex(b).Status)
	}
	if !r.tableExists("zz_p") || !r.tableExists("zz_q") || r.hist() != 2 {
		t.Fatal("A lalu B harus ter-commit berurutan")
	}
}

func TestOutOfOrderWithoutEarlierInFlightStillFails(t *testing.T) {
	r := newRig(t)
	ma := r.mig("A", "CREATE TABLE zz_p(id int)", "DROP TABLE zz_p")
	exA, err := r.svc.Submit(r.ctx, r.uid, r.db, ma)
	r.must(err)
	_ = exA
	b := r.ready(r.mig("B", "CREATE TABLE zz_q(id int)", "DROP TABLE zz_q"))

	r.run(b)
	if st := r.ex(b).Status; st != workflow.StatusFailed {
		t.Fatalf("B tanpa A di antrean harus gagal, bukan %s", st)
	}
	if r.tableExists("zz_q") {
		t.Fatal("SQL B tidak boleh dijalankan")
	}
}
