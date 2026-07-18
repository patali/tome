package store

import "database/sql"

// Settings are the admin-managed delivery settings. The Resend API key lives
// only here (never in env, never echoed by the API).
type Settings struct {
	ResendAPIKey string
	ResendFrom   string
}

const (
	keyResendAPIKey = "resend_api_key"
	keyResendFrom   = "resend_from"
)

func (s *Store) getSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) setSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO settings (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

func (s *Store) GetSettings() (Settings, error) {
	apiKey, err := s.getSetting(keyResendAPIKey)
	if err != nil {
		return Settings{}, err
	}
	from, err := s.getSetting(keyResendFrom)
	if err != nil {
		return Settings{}, err
	}
	return Settings{ResendAPIKey: apiKey, ResendFrom: from}, nil
}

// SetSettings updates only the non-nil fields (nil = leave unchanged; a
// pointer to "" clears the value).
func (s *Store) SetSettings(resendAPIKey, resendFrom *string) error {
	if resendAPIKey != nil {
		if err := s.setSetting(keyResendAPIKey, *resendAPIKey); err != nil {
			return err
		}
	}
	if resendFrom != nil {
		if err := s.setSetting(keyResendFrom, *resendFrom); err != nil {
			return err
		}
	}
	return nil
}
