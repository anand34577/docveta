// Package settings stores runtime configuration editable from the admin UI.
// Plain values live in `settings` (JSON); secrets live encrypted in `secret_settings`.
package settings

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
)

type Store struct {
	pool *pgxpool.Pool
	keys *crypto.Keys
}

func New(pool *pgxpool.Pool, keys *crypto.Keys) *Store { return &Store{pool: pool, keys: keys} }

// Get loads key into v. Returns false if the key is not set.
func (s *Store) Get(ctx context.Context, key string, v any) (bool, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&raw)
	if db.IsNoRows(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, v)
}

func (s *Store) Set(ctx context.Context, key string, v any, by uuid.UUID) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var updatedBy *uuid.UUID
	if by != uuid.Nil {
		updatedBy = &by
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO settings (key, value, updated_by) VALUES ($1,$2,$3)
		ON CONFLICT (key) DO UPDATE SET value=excluded.value, updated_by=excluded.updated_by, updated_at=now()`, key, raw, updatedBy)
	return err
}

// GetSecret returns the decrypted secret or "" if unset.
func (s *Store) GetSecret(ctx context.Context, key string) (string, error) {
	var sealed []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM secret_settings WHERE key=$1`, key).Scan(&sealed)
	if db.IsNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	b, err := s.keys.Decrypt(sealed)
	return string(b), err
}

// SetSecret stores an encrypted secret; an empty value deletes it.
func (s *Store) SetSecret(ctx context.Context, key, value string) error {
	if value == "" {
		_, err := s.pool.Exec(ctx, `DELETE FROM secret_settings WHERE key=$1`, key)
		return err
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO secret_settings (key, value) VALUES ($1,$2)
		ON CONFLICT (key) DO UPDATE SET value=excluded.value, updated_at=now()`, key, s.keys.Encrypt([]byte(value)))
	return err
}

// HasSecret reports whether a secret is set without decrypting it.
func (s *Store) HasSecret(ctx context.Context, key string) bool {
	var ok bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM secret_settings WHERE key=$1)`, key).Scan(&ok)
	return ok
}
