package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Settings is the admin-configurable system settings map.
type Settings map[string]any

// settingRow mirrors the storage row.
type settingRow struct {
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

// GetSettings loads all settings.
func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT key, value FROM system_settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := Settings{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			continue // tolerate old entries
		}
		settings[key] = value
	}
	return settings, rows.Err()
}

// UpdateSettings upserts the given keys.
func (s *Store) UpdateSettings(ctx context.Context, settings Settings) error {
	for key, value := range settings {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encoding setting %s: %w", key, err)
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO system_settings (key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			key, string(encoded), utcNow())
		if err != nil {
			return fmt.Errorf("saving setting %s: %w", key, err)
		}
	}
	return nil
}

// GetSettingBool reads a boolean setting with a fallback.
func (s *Store) GetSettingBool(ctx context.Context, key string, fallback bool) bool {
	var raw string
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM system_settings WHERE key = ?", key).Scan(&raw)
	if err == sql.ErrNoRows {
		return fallback
	}
	if err != nil {
		return fallback
	}
	var value bool
	if json.Unmarshal([]byte(raw), &value) != nil {
		return fallback
	}
	return value
}

// GetSettingFloat reads a float setting with a fallback.
func (s *Store) GetSettingFloat(ctx context.Context, key string, fallback float64) float64 {
	var raw string
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM system_settings WHERE key = ?", key).Scan(&raw)
	if err == sql.ErrNoRows {
		return fallback
	}
	if err != nil {
		return fallback
	}
	var value float64
	if json.Unmarshal([]byte(raw), &value) != nil {
		return fallback
	}
	return value
}
