package access

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access/accessdb"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Answer languages. "user" answers in the language of the question.
const (
	LanguageUser = "user"
	LanguageES   = "es"
	LanguageEN   = "en"
)

// Limits of the settings an administrator can change.
const (
	defaultThreshold     = 0.35
	defaultRetentionDays = 730 // two years, the audit retention of ATLAS.md §15
	minRetentionDays     = 30
	maxRetentionDays     = 3650
)

var answerLanguages = map[string]bool{LanguageUser: true, LanguageES: true, LanguageEN: true}

// Settings are the organization's own choices. The models and network isolation belong to the deployment,
// not to this record.
type Settings struct {
	OrgName            string  `json:"-"`
	NotFoundThreshold  float64 `json:"not_found_threshold"`
	AnswerLanguage     string  `json:"answer_language"`
	RetentionDays      int     `json:"retention_days"`
	RequireCitation    bool    `json:"require_citation"`
	WarnContradictions bool    `json:"warn_contradictions"`
}

// DefaultSettings are what an organization has until it changes something.
func DefaultSettings() Settings {
	return Settings{NotFoundThreshold: defaultThreshold, AnswerLanguage: LanguageUser, RetentionDays: defaultRetentionDays,
		RequireCitation: true, WarnContradictions: true}
}

// SettingsPatch changes some settings. A nil field is left alone.
type SettingsPatch struct {
	OrgName            *string
	NotFoundThreshold  *float64
	AnswerLanguage     *string
	RetentionDays      *int
	RequireCitation    *bool
	WarnContradictions *bool
}

// LoadSettings reads the settings of the tenant that tx is scoped to. Other modules call it to apply them.
func LoadSettings(ctx context.Context, tx pgx.Tx) (Settings, error) {
	row, err := accessdb.New(tx).GetCurrentTenantSettings(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("get settings: %w", err)
	}
	return parseSettings(row.OrgName, row.Settings)
}

// parseSettings lays the stored overrides over the defaults.
func parseSettings(orgName string, stored []byte) (Settings, error) {
	s := DefaultSettings()
	if len(stored) > 0 {
		if err := json.Unmarshal(stored, &s); err != nil {
			return Settings{}, fmt.Errorf("read settings: %w", err)
		}
	}
	s.OrgName = orgName
	return s, nil
}

// Settings returns the organization's settings.
func (s *Service) Settings(ctx context.Context, p Principal) (Settings, error) {
	var out Settings
	err := s.DB.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var loadErr error
		out, loadErr = LoadSettings(ctx, tx)
		return loadErr
	})
	return out, err
}

// UpdateSettings applies a patch and records which settings changed (names only) in the audit log.
func (s *Service) UpdateSettings(ctx context.Context, p Principal, patch SettingsPatch) (Settings, error) {
	var out Settings
	err := s.DB.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var saveErr error
		out, saveErr = s.saveSettings(ctx, tx, scopedPatch{by: p.UserID, patch: patch})
		return saveErr
	})
	return out, err
}

type scopedPatch struct {
	by    uuid.UUID
	patch SettingsPatch
}

func (s *Service) saveSettings(ctx context.Context, tx pgx.Tx, in scopedPatch) (Settings, error) {
	q := accessdb.New(tx)
	row, err := q.LockCurrentTenantSettings(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("lock settings: %w", err)
	}
	cur, err := parseSettings(row.OrgName, row.Settings)
	if err != nil {
		return Settings{}, err
	}
	next, changed, err := applySettings(cur, in.patch)
	if err != nil || len(changed) == 0 {
		return next, err
	}
	stored, _ := json.Marshal(next) // plain fields: cannot fail
	if err := q.SaveCurrentTenantSettings(ctx, accessdb.SaveCurrentTenantSettingsParams{OrgName: next.OrgName, Settings: stored}); err != nil {
		return Settings{}, fmt.Errorf("save settings: %w", err)
	}
	entry := audit.Entry{At: s.Now(), Actor: in.by, Action: "settings.updated", Subject: "settings", Detail: "fields=" + strings.Join(changed, ",")}
	return next, audit.Record(ctx, tx, entry)
}

// applySettings validates the patch and returns the new settings and the names of the ones that changed.
func applySettings(cur Settings, p SettingsPatch) (Settings, []string, error) {
	if err := validateSettings(p); err != nil {
		return cur, nil, err
	}
	next, changed := cur, []string{}
	for _, f := range settingFields(&next, p) {
		if f.set() {
			changed = append(changed, f.name)
		}
	}
	return next, changed, nil
}

// settingField sets one setting from the patch and reports whether its value changed.
type settingField struct {
	name string
	set  func() bool
}

func settingFields(next *Settings, p SettingsPatch) []settingField {
	return []settingField{
		{"org_name", func() bool { return assign(&next.OrgName, trimmed(p.OrgName)) }},
		{"not_found_threshold", func() bool { return assign(&next.NotFoundThreshold, p.NotFoundThreshold) }},
		{"answer_language", func() bool { return assign(&next.AnswerLanguage, p.AnswerLanguage) }},
		{"retention_days", func() bool { return assign(&next.RetentionDays, p.RetentionDays) }},
		{"require_citation", func() bool { return assign(&next.RequireCitation, p.RequireCitation) }},
		{"warn_contradictions", func() bool { return assign(&next.WarnContradictions, p.WarnContradictions) }},
	}
}

// assign writes *value into *dst when a value was given and differs. It reports whether it changed.
func assign[T comparable](dst *T, value *T) bool {
	if value == nil || *value == *dst {
		return false
	}
	*dst = *value
	return true
}

func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

func validateSettings(p SettingsPatch) error {
	checks := []func(SettingsPatch) error{checkOrgName, checkThreshold, checkLanguage, checkRetention}
	for _, check := range checks {
		if err := check(p); err != nil {
			return err
		}
	}
	return nil
}

func checkOrgName(p SettingsPatch) error {
	if p.OrgName != nil && strings.TrimSpace(*p.OrgName) == "" {
		return fmt.Errorf("the organization name cannot be empty: %w", errs.ErrInvalid)
	}
	return nil
}

func checkThreshold(p SettingsPatch) error {
	if p.NotFoundThreshold != nil && (*p.NotFoundThreshold < 0 || *p.NotFoundThreshold > 1) {
		return fmt.Errorf("the threshold is between 0 and 1: %w", errs.ErrInvalid)
	}
	return nil
}

func checkLanguage(p SettingsPatch) error {
	if p.AnswerLanguage != nil && !answerLanguages[*p.AnswerLanguage] {
		return fmt.Errorf("unknown answer language %q: %w", *p.AnswerLanguage, errs.ErrInvalid)
	}
	return nil
}

func checkRetention(p SettingsPatch) error {
	if p.RetentionDays != nil && (*p.RetentionDays < minRetentionDays || *p.RetentionDays > maxRetentionDays) {
		return fmt.Errorf("retention is %d to %d days: %w", minRetentionDays, maxRetentionDays, errs.ErrInvalid)
	}
	return nil
}
