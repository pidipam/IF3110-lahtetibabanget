package approval_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/approval"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/testutil"
)

type fixture struct {
	t *testing.T
	db *pgxpool.Pool
	svc *approval.Service
	conn int64
	approved atomic.Int32
}

func setup(t *testing.T) *fixture {
	db := testutil.NewTestDB(t)
	f := &fixture{t: t, db: db, svc: approval.NewService(db, authz.New(authz.NewPGStore(db))), conn: testutil.SeedConnection(t, db).ID}
	f.svc.OnApproved = func(ctx context.Context, execID int64) { f.approved.Add(1) }
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) user(name string) authz.Principal {
	return authz.Principal{UserID: testutil.SeedUser(f.t, f.db, name, "password123", false)}
}

func (f *fixture) group(name string, level authz.Level, leader *authz.Principal, members ...authz.Principal) int64 {
	f.t.Helper()
	gid := testutil.SeedGroup(f.t, f.db, name)
	if leader != nil {
		members = append(members, *leader)
	}
	for _, m := range members {
		testutil.SeedMember(f.t, f.db, gid, m.UserID)
	}
	if leader != nil {
		f.setLeader(gid, leader.UserID)
	}
	if level != authz.LevelNone {
		testutil.SeedGrant(f.t, f.db, gid, f.conn, level)
	}
	return gid
}

func (f *fixture) setLeader(gid, userID int64) {
	f.exec("UPDATE groups SET leader_id = $2 WHERE id = $1", gid, userID)
}

func (f *fixture) migration(requester authz.Principal) (int64, int64) {
	f.t.Helper()
	ctx := context.Background()
	var mid, eid int64
	if err := f.db.QueryRow(ctx, "INSERT INTO platform_migrations (database_id, title, created_by) VALUES ($1, 'Tambah index', $2) RETURNING id", f.conn, requester.UserID).Scan(&mid); err != nil {
		f.t.Fatal(err)
	}
	f.exec("INSERT INTO platform_migration_revisions (migration_id, revision_number, sql_up, sql_down, checksum, created_by) VALUES ($1, 1, 'SELECT 1', 'SELECT 2', $2, $3)", mid, strings.Repeat("a", 64), requester.UserID)
	if err := f.db.QueryRow(ctx, "INSERT INTO platform_migration_executions (migration_id, database_id, revision_number, kind, status, requested_by) VALUES ($1, $2, 1, 'up', 'pending_review', $3) RETURNING id", mid, f.conn, requester.UserID).Scan(&eid); err != nil {
		f.t.Fatal(err)
	}
	return mid, eid
}

func (f *fixture) newRevision(mid int64, by authz.Principal) int {
	f.t.Helper()
	var rev int
	if err := f.db.QueryRow(context.Background(), "UPDATE platform_migrations SET current_revision = current_revision + 1 WHERE id = $1 RETURNING current_revision", mid).Scan(&rev); err != nil {
		f.t.Fatal(err)
	}
	f.exec("INSERT INTO platform_migration_revisions (migration_id, revision_number, sql_up, sql_down, checksum, created_by) VALUES ($1, $2, 'SELECT 3', 'SELECT 4', $3, $4)", mid, rev, strings.Repeat("b", 64), by.UserID)
	return rev
}

func (f *fixture) status(eid int64) string {
	f.t.Helper()
	var s string
	if err := f.db.QueryRow(context.Background(), "SELECT status FROM platform_migration_executions WHERE id = $1", eid).Scan(&s); err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) votes() int {
	return testutil.Count(f.t, f.db, "SELECT count(*) FROM migration_approvals")
}

func expectErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error %v, want %v", err, want)
	}
}

func TestApproveSingleGroup(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	lead := f.user("lead")
	f.group("analis", authz.LevelEdit, &lead, dev)
	mid, eid := f.migration(dev)

	st, err := f.svc.Status(ctx, eid)
	if err != nil || st.State != approval.StateWaiting || len(st.Groups) != 1 || st.Groups[0].LeaderName != "lead" {
		t.Fatalf("status awal salah: %+v %v", st, err)
	}
	st, err = f.svc.Approve(ctx, lead, mid, 1)
	if err != nil || st.State != approval.StateApproved || st.Execution.Status != "approved" {
		t.Fatalf("approve gagal: %+v %v", st, err)
	}
	if f.status(eid) != "approved" || f.approved.Load() != 1 {
		t.Fatalf("status %q, callback %d", f.status(eid), f.approved.Load())
	}
	if err := f.svc.Check(ctx, eid); err != nil {
		t.Fatalf("Check harus lolos: %v", err)
	}
	_, err = f.svc.Approve(ctx, lead, mid, 1)
	expectErr(t, err, approval.ErrNoPending)
	_, err = f.svc.Approve(ctx, lead, 9999, 1)
	expectErr(t, err, approval.ErrNotFound)
}

func TestApproveManyGroups(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	a := f.user("lead-a")
	b := f.user("lead-b")
	f.group("analis", authz.LevelEdit, &a, dev)
	f.group("ops", authz.LevelView, &b)
	f.group("tanpa-akses", authz.LevelNone, nil)
	mid, eid := f.migration(dev)

	groups, err := f.svc.RequiredApprovers(ctx, f.conn)
	if err != nil || len(groups) != 2 {
		t.Fatalf("grup wajib harus 2 (view dan edit): %+v %v", groups, err)
	}
	st, err := f.svc.Approve(ctx, a, mid, 1)
	if err != nil || st.State != approval.StateWaiting || !strings.Contains(st.Message, "ops") {
		t.Fatalf("setelah satu leader: %+v %v", st, err)
	}
	if f.status(eid) != "pending_review" {
		t.Fatal("belum semua setuju, status harus tetap pending_review")
	}
	_, err = f.svc.Approve(ctx, a, mid, 1)
	expectErr(t, err, approval.ErrAlreadyDecided)
	st, err = f.svc.Approve(ctx, b, mid, 1)
	if err != nil || st.State != approval.StateApproved || f.status(eid) != "approved" {
		t.Fatalf("setelah semua leader: %+v %v", st, err)
	}
}

func TestOneLeaderOfSeveralGroupsDecidesOnce(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	lead := f.user("lead")
	f.group("analis", authz.LevelEdit, &lead, dev)
	f.group("ops", authz.LevelView, &lead)
	mid, eid := f.migration(dev)
	st, err := f.svc.Approve(ctx, lead, mid, 1)
	if err != nil || st.State != approval.StateApproved || f.votes() != 2 || f.status(eid) != "approved" {
		t.Fatalf("satu keputusan harus berlaku untuk semua grup yang dipimpin: %+v %v votes=%d", st, err, f.votes())
	}
}

func TestGroupWithoutLeaderBlocks(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	a := f.user("lead-a")
	opsMember := f.user("ops-member")
	f.group("analis", authz.LevelEdit, &a, dev)
	ops := f.group("ops", authz.LevelView, nil, opsMember)
	mid, eid := f.migration(dev)

	st, err := f.svc.Approve(ctx, a, mid, 1)
	if err != nil || st.State != approval.StateBlocked || !strings.Contains(st.Message, "Grup ops belum punya leader aktif. Minta admin menetapkan leader.") {
		t.Fatalf("harus tertahan dengan pesan jelas: %+v %v", st, err)
	}
	if f.status(eid) != "pending_review" {
		t.Fatal("grup tanpa leader harus menahan proses")
	}
	f.setLeader(ops, opsMember.UserID)
	f.exec("UPDATE users SET is_active = FALSE WHERE id = $1", opsMember.UserID)
	st, _ = f.svc.Status(ctx, eid)
	if st.State != approval.StateBlocked {
		t.Fatalf("leader nonaktif tetap menahan: %s", st.State)
	}
	f.exec("UPDATE users SET is_active = TRUE WHERE id = $1", opsMember.UserID)
	st, err = f.svc.Approve(ctx, opsMember, mid, 1)
	if err != nil || st.State != approval.StateApproved {
		t.Fatalf("setelah leader ditetapkan: %+v %v", st, err)
	}
}

func TestNonLeaderCannotDecide(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	lead := f.user("lead")
	editor := f.user("editor")
	other := f.user("lead-lain")
	admin := authz.Principal{UserID: testutil.SeedUser(t, f.db, "root", "password123", true), IsAdmin: true}
	f.group("analis", authz.LevelEdit, &lead, editor)
	f.group("lain", authz.LevelNone, &other)
	f.group("pengaju", authz.LevelEdit, &dev)
	mid, eid := f.migration(dev)

	for name, p := range map[string]authz.Principal{"anggota edit": editor, "leader grup tanpa akses": other, "admin": admin} {
		_, err := f.svc.Approve(ctx, p, mid, 1)
		if !errors.Is(err, approval.ErrNotApprover) {
			t.Fatalf("%s: %v", name, err)
		}
		_, err = f.svc.Reject(ctx, p, mid, 1, "tidak setuju")
		if !errors.Is(err, approval.ErrNotApprover) {
			t.Fatalf("%s reject: %v", name, err)
		}
	}
	_, err := f.svc.Approve(ctx, dev, mid, 1)
	expectErr(t, err, approval.ErrSelfApproval)
	f.exec("UPDATE users SET is_active = FALSE WHERE id = $1", lead.UserID)
	_, err = f.svc.Approve(ctx, lead, mid, 1)
	expectErr(t, err, approval.ErrNotApprover)
	if f.votes() != 0 || f.status(eid) != "pending_review" {
		t.Fatal("keputusan dari yang tidak berhak tersimpan")
	}
}

func TestRevisionChangeVoidsApproval(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	a := f.user("lead-a")
	b := f.user("lead-b")
	f.group("analis", authz.LevelEdit, &a, dev)
	f.group("ops", authz.LevelView, &b)
	mid, eid := f.migration(dev)
	if _, err := f.svc.Approve(ctx, a, mid, 1); err != nil {
		t.Fatal(err)
	}

	rev := f.newRevision(mid, dev)
	st, _ := f.svc.Status(ctx, eid)
	if st.State != approval.StateStale {
		t.Fatalf("revisi berubah harus stale: %s", st.State)
	}
	_, err := f.svc.Approve(ctx, b, mid, 1)
	expectErr(t, err, approval.ErrRevisionMismatch)

	f.exec("UPDATE platform_migration_executions SET revision_number = $2 WHERE id = $1", eid, rev)
	st, _ = f.svc.Status(ctx, eid)
	if st.State != approval.StateWaiting || st.Groups[0].Decision != approval.DecisionPending || st.Groups[1].Decision != approval.DecisionPending {
		t.Fatalf("persetujuan revisi lama harus hangus: %+v", st)
	}
	_, err = f.svc.Approve(ctx, b, mid, 1)
	expectErr(t, err, approval.ErrRevisionMismatch)
	if _, err := f.svc.Approve(ctx, a, mid, rev); err != nil {
		t.Fatal(err)
	}
	st, err = f.svc.Approve(ctx, b, mid, rev)
	if err != nil || st.State != approval.StateApproved {
		t.Fatalf("persetujuan revisi baru: %+v %v", st, err)
	}
	if f.votes() != 3 {
		t.Fatalf("jejak persetujuan lama harus tetap tersimpan, votes=%d", f.votes())
	}
}

func TestCheckFailsWhenGroupGainsAccessAfterApproval(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	a := f.user("lead-a")
	c := f.user("lead-c")
	f.group("analis", authz.LevelEdit, &a, dev)
	mid, eid := f.migration(dev)
	if _, err := f.svc.Approve(ctx, a, mid, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Check(ctx, eid); err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE platform_migration_executions SET status = 'queued' WHERE id = $1", eid)

	gc := f.group("baru", authz.LevelNone, &c)
	testutil.SeedGrant(t, f.db, gc, f.conn, authz.LevelView)
	err := f.svc.Check(ctx, eid)
	if !errors.Is(err, approval.ErrApprovalStale) || !strings.Contains(err.Error(), "Grup baru") {
		t.Fatalf("Check harus gagal karena grup baru: %v", err)
	}
	if err := f.svc.Reopen(ctx, eid); err != nil || f.status(eid) != "pending_review" {
		t.Fatalf("reopen: %v %s", err, f.status(eid))
	}
	expectErr(t, f.svc.Reopen(ctx, eid), approval.ErrNotReopenable)
	if _, err := f.svc.Approve(ctx, c, mid, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Check(ctx, eid); err != nil {
		t.Fatalf("setelah grup baru setuju: %v", err)
	}

	f.exec("UPDATE groups SET leader_id = NULL WHERE id = $1", gc)
	if err := f.svc.Check(ctx, eid); err != nil {
		t.Fatalf("grup yang sudah setuju tetap sah walau leader dilepas: %v", err)
	}
	f.group("tanpa-leader", authz.LevelView, nil)
	err = f.svc.Check(ctx, eid)
	if !errors.Is(err, approval.ErrApprovalStale) || !strings.Contains(err.Error(), "belum punya leader") {
		t.Fatalf("grup baru tanpa leader harus menggagalkan Check: %v", err)
	}
}

func TestRejectRequiresReason(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	a := f.user("lead-a")
	b := f.user("lead-b")
	f.group("analis", authz.LevelEdit, &a, dev)
	f.group("ops", authz.LevelView, &b)
	mid, eid := f.migration(dev)
	_, err := f.svc.Reject(ctx, a, mid, 1, "   ")
	expectErr(t, err, approval.ErrReasonRequired)
	_, err = f.svc.Reject(ctx, a, mid, 1, strings.Repeat("x", 501))
	expectErr(t, err, approval.ErrReasonTooLong)
	if _, err := f.svc.Approve(ctx, b, mid, 1); err != nil {
		t.Fatal(err)
	}
	st, err := f.svc.Reject(ctx, a, mid, 1, " Index belum diuji di staging. ")
	if err != nil || st.State != approval.StateRejected || st.Message != "Permintaan ditolak: Index belum diuji di staging." {
		t.Fatalf("reject: %+v %v", st, err)
	}
	var reason string
	f.db.QueryRow(ctx, "SELECT reject_reason FROM platform_migration_executions WHERE id = $1", eid).Scan(&reason)
	if f.status(eid) != "rejected" || reason != "Index belum diuji di staging." {
		t.Fatalf("status %s alasan %q", f.status(eid), reason)
	}
	_, err = f.svc.Approve(ctx, b, mid, 1)
	expectErr(t, err, approval.ErrNoPending)
	if err := f.svc.Check(ctx, eid); !errors.Is(err, approval.ErrApprovalStale) {
		t.Fatalf("eksekusi ditolak tidak boleh lolos Check: %v", err)
	}
	if f.approved.Load() != 0 {
		t.Fatal("callback approved tidak boleh terpanggil")
	}
}

func TestRollbackAndForwardNeedApproval(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	lead := f.user("lead")
	f.group("analis", authz.LevelEdit, &lead, dev)
	mid, up := f.migration(dev)
	f.exec("UPDATE platform_migration_executions SET status = 'success' WHERE id = $1", up)
	f.newRevision(mid, dev)

	var rb int64
	if err := f.db.QueryRow(ctx, "INSERT INTO platform_migration_executions (migration_id, database_id, revision_number, kind, status, undoes_execution_id, requested_by) VALUES ($1, $2, 1, 'rollback', 'pending_review', $3, $4) RETURNING id", mid, f.conn, up, dev.UserID).Scan(&rb); err != nil {
		t.Fatal(err)
	}
	st, err := f.svc.Approve(ctx, lead, mid, 1)
	if err != nil || st.State != approval.StateApproved || st.Execution.ID != rb || st.Execution.Kind != "rollback" {
		t.Fatalf("rollback revisi lama tetap bisa disetujui: %+v %v", st, err)
	}
	f.exec("UPDATE platform_migration_executions SET status = 'success' WHERE id = $1", rb)

	var fw int64
	if err := f.db.QueryRow(ctx, "INSERT INTO platform_migration_executions (migration_id, database_id, revision_number, kind, status, requested_by) VALUES ($1, $2, 1, 'forward', 'pending_review', $3) RETURNING id", mid, f.conn, dev.UserID).Scan(&fw); err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.Approve(ctx, lead, mid, 1)
	expectErr(t, err, approval.ErrRevisionMismatch)
	f.exec("UPDATE platform_migration_executions SET revision_number = 2 WHERE id = $1", fw)
	st, err = f.svc.Approve(ctx, lead, mid, 2)
	if err != nil || st.State != approval.StateApproved || st.Execution.Kind != "forward" {
		t.Fatalf("forward: %+v %v", st, err)
	}
}

func TestLeaderRemovedFromGroupIsCleared(t *testing.T) {
	f := setup(t)
	lead := f.user("lead")
	outsider := f.user("luar")
	gid := f.group("analis", authz.LevelEdit, &lead)
	if _, err := f.db.Exec(context.Background(), "UPDATE groups SET leader_id = $2 WHERE id = $1", gid, outsider.UserID); err == nil {
		t.Fatal("leader harus anggota grup")
	}
	f.exec("DELETE FROM group_members WHERE group_id = $1 AND user_id = $2", gid, lead.UserID)
	if testutil.Count(t, f.db, "SELECT count(*) FROM groups WHERE id = $1 AND leader_id IS NULL", gid) != 1 {
		t.Fatal("leader yang dikeluarkan harus dilepas otomatis")
	}
}

func TestConcurrentApprovalsApproveOnce(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	leads := []authz.Principal{f.user("lead-a"), f.user("lead-b"), f.user("lead-c")}
	for i, l := range leads {
		l := l
		f.group("grup-"+string(rune('a'+i)), authz.LevelView, &l, dev)
	}
	mid, eid := f.migration(dev)
	var wg sync.WaitGroup
	errs := make([]error, len(leads))
	for i, l := range leads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.Approve(ctx, l, mid, 1)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.status(eid) != "approved" || f.votes() != 3 || f.approved.Load() != 1 {
		t.Fatalf("status %s votes %d callback %d", f.status(eid), f.votes(), f.approved.Load())
	}
}

func TestPendingLists(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dev := f.user("dev")
	a := f.user("lead-a")
	b := f.user("lead-b")
	viewer := f.user("viewer")
	outsider := f.user("luar")
	f.group("analis", authz.LevelEdit, &a, dev)
	f.group("ops", authz.LevelView, &b, viewer)
	mid, _ := f.migration(dev)

	list, total, err := f.svc.Pending(ctx, a, true, 20, 0)
	if err != nil || total != 1 || len(list) != 1 || list[0].Execution.MigrationID != mid || list[0].Execution.Title != "Tambah index" {
		t.Fatalf("mine leader a: %+v %d %v", list, total, err)
	}
	if _, err := f.svc.Approve(ctx, a, mid, 1); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := f.svc.Pending(ctx, a, true, 20, 0); total != 0 {
		t.Fatal("yang sudah diputuskan tidak boleh muncul di daftar saya")
	}
	if _, total, _ := f.svc.Pending(ctx, b, true, 20, 0); total != 1 {
		t.Fatal("leader b masih harus memutuskan")
	}
	for name, p := range map[string]authz.Principal{"pengaju": dev, "anggota biasa": viewer, "orang luar": outsider} {
		if _, total, _ := f.svc.Pending(ctx, p, true, 20, 0); total != 0 {
			t.Fatalf("%s tidak punya keputusan yang ditunggu", name)
		}
	}
	if _, total, _ := f.svc.Pending(ctx, viewer, false, 20, 0); total != 1 {
		t.Fatal("anggota dengan akses view boleh melihat permintaan yang menunggu")
	}
	if _, total, _ := f.svc.Pending(ctx, outsider, false, 20, 0); total != 0 {
		t.Fatal("orang tanpa akses tidak boleh melihat permintaan")
	}
}
