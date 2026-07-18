package store

import (
	"errors"
	"time"
)

// ErrInviteInvalid covers unknown, already-used, and expired invites — one
// error on purpose, so the API can't be used as a validity oracle.
var ErrInviteInvalid = errors.New("invalid or expired invite")

type Invite struct {
	Code      string  `json:"code"`
	EmailHint string  `json:"emailHint"`
	CreatedAt string  `json:"createdAt"`
	ExpiresAt string  `json:"expiresAt"`
	UsedBy    *string `json:"usedBy"` // redeeming user's email, nil if unused
	UsedAt    *string `json:"usedAt"`
}

const timeLayout = "2006-01-02T15:04:05Z"

func nowUTC() string { return time.Now().UTC().Format(timeLayout) }

func (s *Store) CreateInvite(code, emailHint string, ttl time.Duration) (Invite, error) {
	inv := Invite{
		Code:      code,
		EmailHint: emailHint,
		CreatedAt: nowUTC(),
		ExpiresAt: time.Now().UTC().Add(ttl).Format(timeLayout),
	}
	_, err := s.db.Exec(
		"INSERT INTO invites (code, email_hint, created_at, expires_at) VALUES (?,?,?,?)",
		inv.Code, inv.EmailHint, inv.CreatedAt, inv.ExpiresAt)
	return inv, err
}

func (s *Store) ListInvites() ([]Invite, error) {
	rows, err := s.db.Query(`
		SELECT i.code, i.email_hint, i.created_at, i.expires_at, u.email, i.used_at
		  FROM invites i LEFT JOIN users u ON u.id = i.used_by
		 ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		var inv Invite
		if err := rows.Scan(&inv.Code, &inv.EmailHint, &inv.CreatedAt, &inv.ExpiresAt, &inv.UsedBy, &inv.UsedAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *Store) DeleteInvite(code string) error {
	return s.expectOne(s.db.Exec("DELETE FROM invites WHERE code = ?", code))
}

// Redeem atomically claims an unused, unexpired invite and creates the user.
// Returns ErrInviteInvalid or ErrEmailExists on the respective failures.
func (s *Store) Redeem(code, email, kindleEmail, keyHash, keyPrefix string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var found string
	err = tx.QueryRow(
		"SELECT code FROM invites WHERE code = ? AND used_by IS NULL AND expires_at > ?",
		code, nowUTC()).Scan(&found)
	if err != nil { // sql.ErrNoRows and real errors alike -> generic invalid
		return 0, ErrInviteInvalid
	}

	res, err := tx.Exec(
		"INSERT INTO users (email, kindle_email, api_key_hash, api_key_prefix, is_admin) VALUES (?,?,?,?,0)",
		email, kindleEmail, keyHash, keyPrefix)
	if isEmailConflict(err) {
		return 0, ErrEmailExists
	}
	if err != nil {
		return 0, err
	}
	userID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	claim, err := tx.Exec(
		"UPDATE invites SET used_by = ?, used_at = ? WHERE code = ? AND used_by IS NULL",
		userID, nowUTC(), code)
	if err != nil {
		return 0, err
	}
	if n, _ := claim.RowsAffected(); n == 0 {
		return 0, ErrInviteInvalid
	}
	return userID, tx.Commit()
}
