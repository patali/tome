package store

import "database/sql"

// Settings are the admin-managed delivery and analytics settings. The API keys
// live only here (never in env, never echoed by the API).
type Settings struct {
	ResendAPIKey string
	ResendFrom   string

	// PostHog is optional product analytics, off unless an operator sets a key,
	// and always the operator's own project. See internal/posthog.
	PostHogAPIKey string
	PostHogHost   string
}

const (
	keyResendAPIKey  = "resend_api_key"
	keyResendFrom    = "resend_from"
	keyPostHogAPIKey = "posthog_api_key"
	keyPostHogHost   = "posthog_host"
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
	var out Settings
	for _, f := range []struct {
		key  string
		dest *string
	}{
		{keyResendAPIKey, &out.ResendAPIKey},
		{keyResendFrom, &out.ResendFrom},
		{keyPostHogAPIKey, &out.PostHogAPIKey},
		{keyPostHogHost, &out.PostHogHost},
	} {
		v, err := s.getSetting(f.key)
		if err != nil {
			return Settings{}, err
		}
		*f.dest = v
	}
	return out, nil
}

// SetSettings updates only the non-nil fields (nil = leave unchanged; a
// pointer to "" clears the value).
func (s *Store) SetSettings(resendAPIKey, resendFrom, posthogAPIKey, posthogHost *string) error {
	for _, f := range []struct {
		key   string
		value *string
	}{
		{keyResendAPIKey, resendAPIKey},
		{keyResendFrom, resendFrom},
		{keyPostHogAPIKey, posthogAPIKey},
		{keyPostHogHost, posthogHost},
	} {
		if f.value == nil {
			continue
		}
		if err := s.setSetting(f.key, *f.value); err != nil {
			return err
		}
	}
	return nil
}
