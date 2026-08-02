package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ConversionRetention is how long conversion rows are kept. Matches what
// PRIVACY.md publishes; changing one without the other makes the policy false.
const ConversionRetention = 30 * 24 * time.Hour

// InviteRequestRetention is how long invite requests are kept, handled or not.
// Also published in PRIVACY.md.
const InviteRequestRetention = 90 * 24 * time.Hour

// Conversion is one recorded run. Deliberately carries nothing identifying the
// article — see the table comment in the schema.
type Conversion struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"userId"`
	UserEmail  string `json:"userEmail,omitempty"`
	CreatedAt  string `json:"createdAt"`
	Kind       string `json:"kind"`
	Format     string `json:"format"`
	OK         bool   `json:"ok"`
	Bytes      int64  `json:"bytes"`
	DurationMS int64  `json:"durationMs"`
}

// RecordConversion appends a run. Errors are returned but callers treat them
// as non-fatal: failing a user's conversion because bookkeeping failed would
// be the wrong trade.
func (s *Store) RecordConversion(userID int64, kind, format string, ok bool, bytes, durationMS int64) error {
	_, err := s.db.Exec(
		`INSERT INTO conversions (user_id, kind, format, ok, bytes, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, kind, format, boolToInt(ok), bytes, durationMS)
	return err
}

// ListConversions returns runs newest first. since is inclusive; a zero time
// means no lower bound. userID 0 means all users.
func (s *Store) ListConversions(since time.Time, userID int64, limit int) ([]Conversion, error) {
	q := `SELECT c.id, c.user_id, COALESCE(u.email, ''), c.created_at, c.kind,
	             c.format, c.ok, c.bytes, c.duration_ms
	      FROM conversions c LEFT JOIN users u ON u.id = c.user_id`
	var where []string
	var args []any
	if !since.IsZero() {
		where = append(where, "c.created_at >= ?")
		args = append(args, since.UTC().Format(timeLayout))
	}
	if userID > 0 {
		where = append(where, "c.user_id = ?")
		args = append(args, userID)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY c.created_at DESC, c.id DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Conversion
	for rows.Next() {
		var c Conversion
		var ok int
		if err := rows.Scan(&c.ID, &c.UserID, &c.UserEmail, &c.CreatedAt, &c.Kind,
			&c.Format, &ok, &c.Bytes, &c.DurationMS); err != nil {
			return nil, err
		}
		c.OK = ok != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// UserStat is one user's slice of the activity summary.
type UserStat struct {
	UserID      int64  `json:"userId"`
	Email       string `json:"email"`
	Disabled    bool   `json:"disabled"`
	LastSeenAt  string `json:"lastSeenAt,omitempty"`
	Conversions int64  `json:"conversions"`
	Failed      int64  `json:"failed"`
}

// Stats is the summary behind `tome admin stats`.
type Stats struct {
	Users          int64      `json:"users"`
	UsersDisabled  int64      `json:"usersDisabled"`
	UsersActive30d int64      `json:"usersActive30d"`
	Conversions1d  int64      `json:"conversions1d"`
	Conversions7d  int64      `json:"conversions7d"`
	Conversions30d int64      `json:"conversions30d"`
	Failed30d      int64      `json:"failed30d"`
	InvitesOpen    int64      `json:"invitesOpen"`
	RequestsOpen   int64      `json:"requestsOpen"`
	PerUser        []UserStat `json:"perUser,omitempty"`
}

// Stats computes the summary. withPerUser is optional because the per-user
// breakdown is the only part that grows with the user list.
func (s *Store) Stats(withPerUser bool) (Stats, error) {
	var st Stats
	now := time.Now().UTC()
	ago := func(d time.Duration) string { return now.Add(-d).Format(timeLayout) }

	scalars := []struct {
		dst  *int64
		q    string
		args []any
	}{
		{&st.Users, `SELECT COUNT(*) FROM users`, nil},
		{&st.UsersDisabled, `SELECT COUNT(*) FROM users WHERE disabled = 1`, nil},
		{&st.UsersActive30d, `SELECT COUNT(*) FROM users WHERE last_seen_at >= ?`, []any{ago(30 * 24 * time.Hour)}},
		{&st.Conversions1d, `SELECT COUNT(*) FROM conversions WHERE created_at >= ?`, []any{ago(24 * time.Hour)}},
		{&st.Conversions7d, `SELECT COUNT(*) FROM conversions WHERE created_at >= ?`, []any{ago(7 * 24 * time.Hour)}},
		{&st.Conversions30d, `SELECT COUNT(*) FROM conversions WHERE created_at >= ?`, []any{ago(30 * 24 * time.Hour)}},
		{&st.Failed30d, `SELECT COUNT(*) FROM conversions WHERE ok = 0 AND created_at >= ?`, []any{ago(30 * 24 * time.Hour)}},
		{&st.InvitesOpen, `SELECT COUNT(*) FROM invites WHERE used_at IS NULL AND expires_at > ?`, []any{now.Format(timeLayout)}},
		{&st.RequestsOpen, `SELECT COUNT(*) FROM invite_requests WHERE status = 'pending'`, nil},
	}
	for _, sc := range scalars {
		if err := s.db.QueryRow(sc.q, sc.args...).Scan(sc.dst); err != nil {
			return st, fmt.Errorf("stats: %w", err)
		}
	}
	if !withPerUser {
		return st, nil
	}

	rows, err := s.db.Query(
		`SELECT u.id, u.email, u.disabled, COALESCE(u.last_seen_at, ''),
		        COUNT(c.id), COALESCE(SUM(CASE WHEN c.ok = 0 THEN 1 ELSE 0 END), 0)
		 FROM users u
		 LEFT JOIN conversions c ON c.user_id = u.id AND c.created_at >= ?
		 GROUP BY u.id
		 ORDER BY COUNT(c.id) DESC, u.email`, ago(30*24*time.Hour))
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var u UserStat
		var disabled int
		if err := rows.Scan(&u.UserID, &u.Email, &disabled, &u.LastSeenAt, &u.Conversions, &u.Failed); err != nil {
			return st, err
		}
		u.Disabled = disabled != 0
		st.PerUser = append(st.PerUser, u)
	}
	return st, rows.Err()
}

// TouchUser records that a key was used. Best-effort: a write failure must not
// fail the request that triggered it.
func (s *Store) TouchUser(id int64) {
	_, _ = s.db.Exec(`UPDATE users SET last_seen_at = ? WHERE id = ?`, nowUTC(), id)
}

// InviteRequest is one address waiting on an invite.
type InviteRequest struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	CreatedAt string `json:"createdAt"`
	Status    string `json:"status"`
	HandledAt string `json:"handledAt,omitempty"`
}

// AddInviteRequest records a request. A repeat from an address that is already
// waiting refreshes the existing row rather than queueing a second one — the
// operator has one decision to make either way.
func (s *Store) AddInviteRequest(email string) error {
	_, err := s.db.Exec(
		`INSERT INTO invite_requests (email) VALUES (?)
		 ON CONFLICT(email) WHERE status = 'pending'
		 DO UPDATE SET created_at = excluded.created_at`, email)
	return err
}

// ListInviteRequests returns pending requests oldest first — the order you
// would work through them. all includes handled ones, newest first.
func (s *Store) ListInviteRequests(all bool) ([]InviteRequest, error) {
	q := `SELECT id, email, created_at, status, COALESCE(handled_at, '')
	      FROM invite_requests WHERE status = 'pending' ORDER BY created_at ASC`
	if all {
		q = `SELECT id, email, created_at, status, COALESCE(handled_at, '')
		     FROM invite_requests ORDER BY created_at DESC`
	}
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InviteRequest
	for rows.Next() {
		var r InviteRequest
		if err := rows.Scan(&r.ID, &r.Email, &r.CreatedAt, &r.Status, &r.HandledAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InviteRequestByRef finds a pending request by numeric id or by email, so the
// CLI can take whichever the operator has to hand.
func (s *Store) InviteRequestByRef(ref string) (*InviteRequest, error) {
	var r InviteRequest
	err := s.db.QueryRow(
		`SELECT id, email, created_at, status, COALESCE(handled_at, '')
		 FROM invite_requests
		 WHERE status = 'pending' AND (CAST(id AS TEXT) = ? OR email = ?)
		 ORDER BY created_at ASC LIMIT 1`, ref, ref).
		Scan(&r.ID, &r.Email, &r.CreatedAt, &r.Status, &r.HandledAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SetInviteRequestStatus marks a request handled.
func (s *Store) SetInviteRequestStatus(id int64, status string) error {
	return s.expectOne(s.db.Exec(
		`UPDATE invite_requests SET status = ?, handled_at = ? WHERE id = ?`,
		status, nowUTC(), id))
}

// Sweep deletes data past its published retention. Returns rows removed, so a
// caller can log something honest about what it did.
func (s *Store) Sweep() (conversions, requests, invites int64, err error) {
	now := time.Now().UTC()
	del := func(q string, arg any) (int64, error) {
		res, err := s.db.Exec(q, arg)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	if conversions, err = del(`DELETE FROM conversions WHERE created_at < ?`,
		now.Add(-ConversionRetention).Format(timeLayout)); err != nil {
		return
	}
	if requests, err = del(`DELETE FROM invite_requests WHERE created_at < ?`,
		now.Add(-InviteRequestRetention).Format(timeLayout)); err != nil {
		return
	}
	// Expired and never redeemed: the code is dead, so the address hint has no
	// further purpose.
	invites, err = del(`DELETE FROM invites WHERE used_at IS NULL AND expires_at < ?`,
		now.Format(timeLayout))
	return
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
