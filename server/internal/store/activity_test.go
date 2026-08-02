package store

import (
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func testUser(t *testing.T, st *Store, email string) int64 {
	t.Helper()
	id, err := st.CreateUser(email, "k@kindle.com", "hash-"+email, "tome_x", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

// backdate rewrites a row's timestamp so retention can be tested without
// waiting 30 days.
func backdate(t *testing.T, st *Store, table string, id int64, age time.Duration) {
	t.Helper()
	ts := time.Now().UTC().Add(-age).Format(timeLayout)
	if _, err := st.db.Exec(`UPDATE `+table+` SET created_at = ? WHERE id = ?`, ts, id); err != nil {
		t.Fatalf("backdate %s: %v", table, err)
	}
}

func TestConversionsRecordAndFilter(t *testing.T) {
	st := testStore(t)
	alice := testUser(t, st, "alice@example.com")
	bob := testUser(t, st, "bob@example.com")

	for _, c := range []struct {
		user int64
		ok   bool
	}{{alice, true}, {alice, false}, {bob, true}} {
		if err := st.RecordConversion(c.user, "convert", "pdf", c.ok, 1234, 56); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	all, err := st.ListConversions(time.Time{}, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 rows, got %d", len(all))
	}
	// The join must resolve the address; the CLI prints it and an empty column
	// would send the operator to `users list` to decode a row id.
	if all[0].UserEmail == "" {
		t.Error("want the user's email resolved on the row")
	}

	mine, err := st.ListConversions(time.Time{}, alice, 0)
	if err != nil {
		t.Fatalf("list by user: %v", err)
	}
	if len(mine) != 2 {
		t.Errorf("want 2 rows for alice, got %d", len(mine))
	}

	limited, err := st.ListConversions(time.Time{}, 0, 1)
	if err != nil {
		t.Fatalf("list with limit: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("want limit respected, got %d rows", len(limited))
	}
}

func TestConversionsSinceExcludesOlder(t *testing.T) {
	st := testStore(t)
	u := testUser(t, st, "a@example.com")
	if err := st.RecordConversion(u, "convert", "pdf", true, 0, 0); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.ListConversions(time.Time{}, 0, 0)
	backdate(t, st, "conversions", rows[0].ID, 48*time.Hour)

	recent, err := st.ListConversions(time.Now().Add(-24*time.Hour), 0, 0)
	if err != nil {
		t.Fatalf("list since: %v", err)
	}
	if len(recent) != 0 {
		t.Errorf("want the 48h-old row excluded by a 24h window, got %d", len(recent))
	}
}

// A repeat request from someone already waiting is the same one decision for
// the operator, so it must not queue a second row.
func TestInviteRequestDeduplicatesWhilePending(t *testing.T) {
	st := testStore(t)
	for i := 0; i < 3; i++ {
		if err := st.AddInviteRequest("dup@example.com"); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	pending, err := st.ListInviteRequests(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("want 1 pending row after 3 submissions, got %d", len(pending))
	}

	// Once handled, the address may legitimately ask again.
	if err := st.SetInviteRequestStatus(pending[0].ID, "dismissed"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddInviteRequest("dup@example.com"); err != nil {
		t.Fatalf("re-request after dismissal: %v", err)
	}
	pending, _ = st.ListInviteRequests(false)
	if len(pending) != 1 {
		t.Errorf("want a fresh pending row after dismissal, got %d", len(pending))
	}
	all, _ := st.ListInviteRequests(true)
	if len(all) != 2 {
		t.Errorf("want both rows with --all, got %d", len(all))
	}
}

func TestInviteRequestByRefAcceptsIDOrEmail(t *testing.T) {
	st := testStore(t)
	if err := st.AddInviteRequest("ref@example.com"); err != nil {
		t.Fatal(err)
	}
	byEmail, err := st.InviteRequestByRef("ref@example.com")
	if err != nil {
		t.Fatalf("by email: %v", err)
	}
	byID, err := st.InviteRequestByRef("1")
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if byEmail.ID != byID.ID {
		t.Errorf("id and email lookups disagree: %d vs %d", byEmail.ID, byID.ID)
	}
	if _, err := st.InviteRequestByRef("nobody@example.com"); err != ErrNotFound {
		t.Errorf("want ErrNotFound for an unknown ref, got %v", err)
	}
}

// The published retention is only real if the sweep enforces it.
func TestSweepEnforcesRetention(t *testing.T) {
	st := testStore(t)
	u := testUser(t, st, "s@example.com")

	if err := st.RecordConversion(u, "convert", "pdf", true, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordConversion(u, "convert", "pdf", true, 0, 0); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.ListConversions(time.Time{}, 0, 0)
	backdate(t, st, "conversions", rows[0].ID, ConversionRetention+24*time.Hour)

	if err := st.AddInviteRequest("old@example.com"); err != nil {
		t.Fatal(err)
	}
	reqs, _ := st.ListInviteRequests(true)
	backdate(t, st, "invite_requests", reqs[0].ID, InviteRequestRetention+24*time.Hour)

	if _, err := st.CreateInvite("dead-code", "x@example.com", -time.Hour); err != nil {
		t.Fatal(err)
	}

	conversions, requests, invites, err := st.Sweep()
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if conversions != 1 {
		t.Errorf("want 1 conversion swept, got %d", conversions)
	}
	if requests != 1 {
		t.Errorf("want 1 request swept, got %d", requests)
	}
	if invites != 1 {
		t.Errorf("want 1 expired invite swept, got %d", invites)
	}

	left, _ := st.ListConversions(time.Time{}, 0, 0)
	if len(left) != 1 {
		t.Errorf("want the in-retention conversion kept, got %d rows", len(left))
	}
}

func TestStatsCountsAndLastSeen(t *testing.T) {
	st := testStore(t)
	alice := testUser(t, st, "alice@example.com")
	bob := testUser(t, st, "bob@example.com")
	if err := st.SetDisabled(bob, true); err != nil {
		t.Fatal(err)
	}
	_ = st.RecordConversion(alice, "convert", "pdf", true, 0, 0)
	_ = st.RecordConversion(alice, "send", "pdf", false, 0, 0)
	_ = st.AddInviteRequest("waiting@example.com")

	// Nobody has authenticated yet, so nobody is active.
	before, err := st.Stats(false)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if before.UsersActive30d != 0 {
		t.Errorf("want 0 active before any request, got %d", before.UsersActive30d)
	}

	st.TouchUser(alice)
	got, err := st.Stats(true)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got.Users != 2 || got.UsersDisabled != 1 || got.UsersActive30d != 1 {
		t.Errorf("users: got %d total / %d disabled / %d active",
			got.Users, got.UsersDisabled, got.UsersActive30d)
	}
	if got.Conversions30d != 2 || got.Failed30d != 1 {
		t.Errorf("conversions: got %d in 30d, %d failed", got.Conversions30d, got.Failed30d)
	}
	if got.RequestsOpen != 1 {
		t.Errorf("want 1 open request, got %d", got.RequestsOpen)
	}
	if len(got.PerUser) != 2 {
		t.Fatalf("want a row per user, got %d", len(got.PerUser))
	}
	// Ordered by volume, so the busiest user is the one you see first.
	if got.PerUser[0].Email != "alice@example.com" || got.PerUser[0].Conversions != 2 {
		t.Errorf("want alice first with 2, got %s with %d",
			got.PerUser[0].Email, got.PerUser[0].Conversions)
	}
}
