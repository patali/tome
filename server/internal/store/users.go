package store

import (
	"database/sql"
	"errors"
	"strings"
)

// ErrNotFound is returned when a lookup matches nothing.
var ErrNotFound = errors.New("not found")

// ErrEmailExists is returned when creating a user with a taken email.
var ErrEmailExists = errors.New("email already exists")

type User struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	KindleEmail string `json:"kindleEmail"`
	KeyPrefix   string `json:"keyPrefix"`
	IsAdmin     bool   `json:"isAdmin"`
	Disabled    bool   `json:"disabled"`
	CreatedAt   string `json:"createdAt"`
}

const userCols = "id, email, kindle_email, api_key_prefix, is_admin, disabled, created_at"

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.KindleEmail, &u.KeyPrefix, &u.IsAdmin, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func isEmailConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: users.email")
}

// CreateUser inserts a user; keyHash/keyPrefix come from auth.NewAPIKey.
func (s *Store) CreateUser(email, kindleEmail, keyHash, keyPrefix string, isAdmin bool) (int64, error) {
	res, err := s.db.Exec(
		"INSERT INTO users (email, kindle_email, api_key_hash, api_key_prefix, is_admin) VALUES (?,?,?,?,?)",
		email, kindleEmail, keyHash, keyPrefix, isAdmin)
	if isEmailConflict(err) {
		return 0, ErrEmailExists
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UserByKeyHash resolves an API key (already hashed) to its user.
func (s *Store) UserByKeyHash(hash string) (*User, error) {
	return scanUser(s.db.QueryRow("SELECT "+userCols+" FROM users WHERE api_key_hash = ?", hash))
}

func (s *Store) UserByID(id int64) (*User, error) {
	return scanUser(s.db.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", id))
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query("SELECT " + userCols + " FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// AdminExists reports whether any admin account is present (bootstrap check).
func (s *Store) AdminExists() (bool, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&n)
	return n > 0, err
}

// FirstAdmin returns the (oldest) admin user, for key recovery.
func (s *Store) FirstAdmin() (*User, error) {
	return scanUser(s.db.QueryRow("SELECT " + userCols + " FROM users WHERE is_admin = 1 ORDER BY id LIMIT 1"))
}

func (s *Store) SetDisabled(id int64, disabled bool) error {
	return s.expectOne(s.db.Exec("UPDATE users SET disabled = ? WHERE id = ?", disabled, id))
}

// RotateKey replaces a user's API key hash; the old key stops working at once.
func (s *Store) RotateKey(id int64, newHash, newPrefix string) error {
	return s.expectOne(s.db.Exec("UPDATE users SET api_key_hash = ?, api_key_prefix = ? WHERE id = ?", newHash, newPrefix, id))
}

func (s *Store) UpdateKindleEmail(id int64, kindleEmail string) error {
	return s.expectOne(s.db.Exec("UPDATE users SET kindle_email = ? WHERE id = ?", kindleEmail, id))
}

func (s *Store) expectOne(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
