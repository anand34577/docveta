package ai

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/audit"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/spaces"
)

// Service owns provider configuration and decides which provider may see which space.
type Service struct {
	pool   *pgxpool.Pool
	keys   *crypto.Keys
	spaces *spaces.Service
	audit  *audit.Log
	vec    vectorState // pgvector support, detected at run time
}

func NewService(pool *pgxpool.Pool, keys *crypto.Keys, sp *spaces.Service, al *audit.Log) *Service {
	return &Service{pool: pool, keys: keys, spaces: sp, audit: al}
}

// Reasons the AI isn't used for a space. Callers treat them as "skip quietly".
var (
	ErrOff      = errors.New("AI is off for this space")
	ErrNoModel  = errors.New("no AI provider is set up")
	ErrPolicy   = errors.New("this space only allows AI that runs on your own hardware")
	ErrNoEmbeds = errors.New("the AI provider has no embedding model")
)

type ProviderInput struct {
	Name           *string `json:"name"`
	BaseURL        *string `json:"base_url"`
	APIKey         *string `json:"api_key"` // write-only; empty string clears
	ChatModel      *string `json:"chat_model"`
	EmbeddingModel *string `json:"embedding_model"`
	IsLocal        *bool   `json:"is_local"`
	IsDefault      *bool   `json:"is_default"`
	Enabled        *bool   `json:"enabled"`
	TimeoutSeconds *int    `json:"timeout_seconds"`
	MaxConcurrency *int    `json:"max_concurrency"`
}

func (in *ProviderInput) validate(create bool) error {
	var v apperr.Validation
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		in.Name = &n
		if n == "" || utf8.RuneCountInString(n) > 60 {
			v.Add("name", "Name must be 1–60 characters")
		}
	} else if create {
		v.Add("name", "Name is required")
	}
	if in.BaseURL != nil {
		u, err := url.Parse(strings.TrimSpace(*in.BaseURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			v.Add("base_url", "Enter the server's address, e.g. http://localhost:11434/v1")
		} else {
			s := strings.TrimRight(u.String(), "/")
			in.BaseURL = &s
		}
	} else if create {
		v.Add("base_url", "Address is required")
	}
	if in.TimeoutSeconds != nil && (*in.TimeoutSeconds < 5 || *in.TimeoutSeconds > 900) {
		v.Add("timeout_seconds", "Between 5 and 900 seconds")
	}
	if in.MaxConcurrency != nil && (*in.MaxConcurrency < 1 || *in.MaxConcurrency > 32) {
		v.Add("max_concurrency", "Between 1 and 32")
	}
	return v.Err()
}

const providerCols = `id, name, base_url, api_key_enc, chat_model, embedding_model, is_local, is_default, enabled, timeout_seconds,
	max_concurrency, last_error, last_ok_at`

func (s *Service) scanProvider(row pgx.Row) (*Provider, error) {
	var p Provider
	var key []byte
	if err := row.Scan(&p.ID, &p.Name, &p.BaseURL, &key, &p.ChatModel, &p.EmbeddingModel, &p.IsLocal, &p.IsDefault, &p.Enabled,
		&p.TimeoutSeconds, &p.MaxConcurrency, &p.LastError, &p.LastOKAt); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("AI provider")
		}
		return nil, err
	}
	if len(key) > 0 {
		p.HasAPIKey = true
		if b, err := s.keys.Decrypt(key); err == nil {
			p.apiKey = string(b)
		}
	}
	return &p, nil
}

func (s *Service) ListProviders(ctx context.Context, p *auth.Principal) ([]*Provider, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	rows, err := s.pool.Query(ctx, `SELECT `+providerCols+` FROM ai_providers ORDER BY is_default DESC, lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Provider{}
	for rows.Next() {
		pr, err := s.scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

func (s *Service) getProvider(ctx context.Context, id uuid.UUID) (*Provider, error) {
	return s.scanProvider(s.pool.QueryRow(ctx, `SELECT `+providerCols+` FROM ai_providers WHERE id=$1`, id))
}

func (s *Service) CreateProvider(ctx context.Context, p *auth.Principal, in ProviderInput) (*Provider, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	if err := in.validate(true); err != nil {
		return nil, err
	}
	id := uuid.Must(uuid.NewV7())
	var key []byte
	if in.APIKey != nil && *in.APIKey != "" {
		key = s.keys.Encrypt([]byte(*in.APIKey))
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ai_providers`).Scan(&n); err != nil {
			return err
		}
		makeDefault := (in.IsDefault != nil && *in.IsDefault) || n == 0 // the first one is the default
		if makeDefault {
			if _, err := tx.Exec(ctx, `UPDATE ai_providers SET is_default=false WHERE is_default`); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO ai_providers (id, name, base_url, api_key_enc, chat_model, embedding_model, is_local, is_default, enabled, timeout_seconds, max_concurrency)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, *in.Name, *in.BaseURL, key, deref(in.ChatModel, ""), deref(in.EmbeddingModel, ""),
			deref(in.IsLocal, false), makeDefault, deref(in.Enabled, true), deref(in.TimeoutSeconds, 90), deref(in.MaxConcurrency, 2))
		return err
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, apperr.Conflict("name_taken", "A provider with this name already exists")
		}
		return nil, err
	}
	s.audit.Record(ctx, nil, "ai.provider_create", "ai_provider", id.String(), map[string]any{"name": *in.Name, "base_url": *in.BaseURL})
	return s.getProvider(ctx, id)
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func (s *Service) UpdateProvider(ctx context.Context, p *auth.Principal, id uuid.UUID, in ProviderInput) (*Provider, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	if err := in.validate(false); err != nil {
		return nil, err
	}
	if _, err := s.getProvider(ctx, id); err != nil {
		return nil, err
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if in.IsDefault != nil && *in.IsDefault {
			if _, err := tx.Exec(ctx, `UPDATE ai_providers SET is_default=false WHERE is_default AND id<>$1`, id); err != nil {
				return err
			}
		}
		if in.APIKey != nil {
			var key []byte
			if *in.APIKey != "" {
				key = s.keys.Encrypt([]byte(*in.APIKey))
			}
			if _, err := tx.Exec(ctx, `UPDATE ai_providers SET api_key_enc=$2 WHERE id=$1`, id, key); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE ai_providers SET name=coalesce($2,name), base_url=coalesce($3,base_url), chat_model=coalesce($4,chat_model),
			embedding_model=coalesce($5,embedding_model), is_local=coalesce($6,is_local), is_default=coalesce($7,is_default), enabled=coalesce($8,enabled),
			timeout_seconds=coalesce($9,timeout_seconds), max_concurrency=coalesce($10,max_concurrency), updated_at=now() WHERE id=$1`,
			id, in.Name, in.BaseURL, in.ChatModel, in.EmbeddingModel, in.IsLocal, in.IsDefault, in.Enabled, in.TimeoutSeconds, in.MaxConcurrency)
		return err
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, apperr.Conflict("name_taken", "A provider with this name already exists")
		}
		return nil, err
	}
	s.audit.Record(ctx, nil, "ai.provider_update", "ai_provider", id.String(), nil)
	return s.getProvider(ctx, id)
}

func (s *Service) DeleteProvider(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM ai_providers WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("AI provider")
	}
	s.audit.Record(ctx, nil, "ai.provider_delete", "ai_provider", id.String(), nil)
	return nil
}

// TestResult is the outcome of "test connection".
type TestResult struct {
	OK     bool     `json:"ok"`
	Error  string   `json:"error,omitempty"`
	Models []string `json:"models"`
	Chat   *bool    `json:"chat_model_found,omitempty"`
	Embed  *bool    `json:"embedding_model_found,omitempty"`
}

func (s *Service) TestProvider(ctx context.Context, p *auth.Principal, id uuid.UUID) (*TestResult, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	pr, err := s.getProvider(ctx, id)
	if err != nil {
		return nil, err
	}
	models, err := NewClient(*pr).Models(ctx)
	res := &TestResult{Models: []string{}}
	if err != nil {
		res.Error = err.Error()
		_, _ = s.pool.Exec(ctx, `UPDATE ai_providers SET last_error=$2 WHERE id=$1`, id, truncate(res.Error, 300))
		return res, nil
	}
	res.OK, res.Models = true, models
	has := func(m string) *bool {
		if m == "" {
			return nil
		}
		ok := false
		for _, x := range models {
			ok = ok || x == m || strings.HasPrefix(x, m+":")
		}
		return &ok
	}
	res.Chat, res.Embed = has(pr.ChatModel), has(pr.EmbeddingModel)
	_, _ = s.pool.Exec(ctx, `UPDATE ai_providers SET last_error='', last_ok_at=now() WHERE id=$1`, id)
	return res, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

// markResult records the outcome of a real call, so Administration shows what's wrong.
func (s *Service) markResult(ctx context.Context, id uuid.UUID, err error) {
	if err == nil {
		_, _ = s.pool.Exec(ctx, `UPDATE ai_providers SET last_error='', last_ok_at=now() WHERE id=$1`, id)
		return
	}
	_, _ = s.pool.Exec(ctx, `UPDATE ai_providers SET last_error=$2 WHERE id=$1`, id, truncate(err.Error(), 300))
}

// forSpace returns the provider allowed for a space's documents: none when the space's
// AI policy is off, only a provider on your own hardware for "local only".
func (s *Service) forSpace(ctx context.Context, spaceID uuid.UUID, needEmbeddings bool) (*Provider, *Client, error) {
	var policy string
	if err := s.pool.QueryRow(ctx, `SELECT ai_policy FROM spaces WHERE id=$1`, spaceID).Scan(&policy); err != nil {
		return nil, nil, err
	}
	if policy == "off" {
		return nil, nil, ErrOff
	}
	pr, err := s.scanProvider(s.pool.QueryRow(ctx, `SELECT `+providerCols+` FROM ai_providers WHERE enabled
		ORDER BY is_default DESC, created_at LIMIT 1`))
	if apperr.IsKind(err, apperr.KindNotFound) {
		return nil, nil, ErrNoModel
	}
	if err != nil {
		return nil, nil, err
	}
	if policy == "local_only" && !pr.IsLocal {
		return nil, nil, ErrPolicy
	}
	if needEmbeddings && pr.EmbeddingModel == "" {
		return nil, nil, ErrNoEmbeds
	}
	if !needEmbeddings && pr.ChatModel == "" {
		return nil, nil, ErrNoModel
	}
	return pr, NewClient(*pr), nil
}

// allowedSpaces filters spaces to those whose policy lets the default provider see them.
func (s *Service) allowedSpaces(ctx context.Context, ids []uuid.UUID, needEmbeddings bool) ([]uuid.UUID, *Provider, *Client, error) {
	var pr *Provider
	var cl *Client
	var out []uuid.UUID
	for _, id := range ids {
		p, c, err := s.forSpace(ctx, id, needEmbeddings)
		if err != nil {
			if errors.Is(err, ErrOff) || errors.Is(err, ErrPolicy) {
				continue
			}
			if len(out) == 0 && pr == nil {
				return nil, nil, nil, err
			}
			continue
		}
		pr, cl = p, c
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, nil, nil, ErrOff
	}
	return out, pr, cl, nil
}

// Status tells the interface which AI features to offer.
type Status struct {
	Enabled    bool `json:"enabled"`
	Chat       bool `json:"chat"`
	Embeddings bool `json:"embeddings"`
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	st := &Status{}
	err := s.pool.QueryRow(ctx, `SELECT count(*) > 0, coalesce(bool_or(chat_model <> ''), false), coalesce(bool_or(embedding_model <> ''), false)
		FROM ai_providers WHERE enabled`).Scan(&st.Enabled, &st.Chat, &st.Embeddings)
	return st, err
}
