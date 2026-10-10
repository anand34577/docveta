package ai

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
)

// Tuning is what an administrator can adjust about the AI for the whole server
// (Administration → AI). What a space decides for itself (how sure the AI must be, whether
// it may make up tags and types) lives on the space.
type Tuning struct {
	// CommonTypes are the document types offered to the model next to a space's own.
	CommonTypes []string `json:"common_types"`
	// AskSources is how many passages an answer is made from; AskSourcesPerDocument how
	// many of them may come from one document.
	AskSources            int `json:"ask_sources"`
	AskSourcesPerDocument int `json:"ask_sources_per_document"`
	// SuggestTextTokens is the most document text sent for suggestions, however large the
	// provider's context window: more reads further into long documents, and takes longer.
	SuggestTextTokens int `json:"suggest_text_tokens"`
}

// DefaultTuning is what applies until an administrator changes it.
func DefaultTuning() Tuning {
	return Tuning{CommonTypes: append([]string{}, commonTypes...), AskSources: 8, AskSourcesPerDocument: 3, SuggestTextTokens: classifyTextTokens}
}

const tuningKey = "ai.tuning"

func (t *Tuning) validate() error {
	var v apperr.Validation
	seen := map[string]bool{}
	types := []string{}
	for _, n := range t.CommonTypes {
		n = strings.Join(strings.Fields(n), " ")
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		if cleanName(n, 60) == "" {
			v.Add("common_types", "“%s…” is too long for a document type (60 characters at most)", string([]rune(n)[:30]))
			continue
		}
		seen[strings.ToLower(n)] = true
		types = append(types, n)
	}
	if len(types) > 60 {
		v.Add("common_types", "At most 60 document types")
	}
	t.CommonTypes = types
	if t.AskSources < 2 || t.AskSources > 20 {
		v.Add("ask_sources", "Between 2 and 20")
	}
	if t.AskSourcesPerDocument < 1 || t.AskSourcesPerDocument > t.AskSources {
		v.Add("ask_sources_per_document", "Between 1 and the number of passages")
	}
	if t.SuggestTextTokens < 500 || t.SuggestTextTokens > 100_000 {
		v.Add("suggest_text_tokens", "Between 500 and 100,000 tokens")
	}
	return v.Err()
}

// tuning returns the saved settings, or the defaults when nothing was saved or it can't be
// read: a broken setting must not stop the AI from working.
func (s *Service) tuning(ctx context.Context) Tuning {
	t := DefaultTuning()
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, tuningKey).Scan(&raw); err != nil {
		return t
	}
	var saved Tuning
	if json.Unmarshal(raw, &saved) != nil || saved.validate() != nil {
		return t
	}
	return saved
}

func (s *Service) GetTuning(ctx context.Context, p *auth.Principal) (*Tuning, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	t := s.tuning(ctx)
	return &t, nil
}

// SetTuning replaces the settings. Fields left out keep their default.
func (s *Service) SetTuning(ctx context.Context, p *auth.Principal, raw json.RawMessage) (*Tuning, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	t := DefaultTuning()
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, apperr.Invalid("body", "Invalid settings")
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(t)
	if _, err := s.pool.Exec(ctx, `INSERT INTO settings (key, value, updated_by) VALUES ($1,$2,$3)
		ON CONFLICT (key) DO UPDATE SET value=excluded.value, updated_by=excluded.updated_by, updated_at=now()`, tuningKey, b, p.UserID); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, nil, "ai.tuning", "settings", tuningKey, nil)
	return &t, nil
}

// ResetTuning goes back to the defaults.
func (s *Service) ResetTuning(ctx context.Context, p *auth.Principal) (*Tuning, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM settings WHERE key=$1`, tuningKey); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, nil, "ai.tuning_reset", "settings", tuningKey, nil)
	t := DefaultTuning()
	return &t, nil
}
