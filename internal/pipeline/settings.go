package pipeline

import (
	"context"
	"slices"
	"strings"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/settings"
)

const settingsKey = "processing"

// Settings is the default processing profile (Admin → Processing). DESIGN §11.6.
type Settings struct {
	// PreferTags: OCR tasks are first offered only to workers advertising all these tags
	// (e.g. ["npu"]). After FallbackAfterMinutes any capable worker may take them.
	PreferTags           []string `json:"prefer_tags"`
	FallbackAfterMinutes int      `json:"fallback_after_minutes"`
	PageBatchSize        int      `json:"page_batch_size"`
	SkipOCRWithText      bool     `json:"skip_ocr_with_text"`
	Archive              bool     `json:"archive"`
	MaxAttempts          int      `json:"max_attempts"`
	LeaseSeconds         int      `json:"lease_seconds"`
	WorkerOfflineMinutes int      `json:"worker_offline_minutes"`
}

func DefaultSettings() Settings {
	return Settings{
		PreferTags:           []string{"npu"},
		FallbackAfterMinutes: 15,
		PageBatchSize:        10,
		SkipOCRWithText:      true,
		Archive:              true,
		MaxAttempts:          3,
		LeaseSeconds:         90,
		WorkerOfflineMinutes: 10,
	}
}

func (s *Settings) normalize() {
	d := DefaultSettings()
	if s.FallbackAfterMinutes < 0 {
		s.FallbackAfterMinutes = d.FallbackAfterMinutes
	}
	if s.PageBatchSize < 1 || s.PageBatchSize > 200 {
		s.PageBatchSize = d.PageBatchSize
	}
	if s.MaxAttempts < 1 || s.MaxAttempts > 10 {
		s.MaxAttempts = d.MaxAttempts
	}
	if s.LeaseSeconds < 30 || s.LeaseSeconds > 3600 {
		s.LeaseSeconds = d.LeaseSeconds
	}
	if s.WorkerOfflineMinutes < 2 {
		s.WorkerOfflineMinutes = d.WorkerOfflineMinutes
	}
	var tags []string
	for _, t := range s.PreferTags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	s.PreferTags = tags
	if s.PreferTags == nil {
		s.PreferTags = []string{}
	}
}

func loadSettings(ctx context.Context, st *settings.Store) Settings {
	s := DefaultSettings()
	_, _ = st.Get(ctx, settingsKey, &s)
	s.normalize()
	return s
}

func (svc *Service) Settings(ctx context.Context, p *auth.Principal) (Settings, error) {
	if !p.Admin() {
		return Settings{}, apperr.Forbidden("")
	}
	return loadSettings(ctx, svc.settings), nil
}

func (svc *Service) SetSettings(ctx context.Context, p *auth.Principal, s Settings) (Settings, error) {
	if !p.Admin() {
		return Settings{}, apperr.Forbidden("")
	}
	s.normalize()
	if err := svc.settings.Set(ctx, settingsKey, s, p.UserID); err != nil {
		return Settings{}, err
	}
	return s, nil
}
