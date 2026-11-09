package access

import (
	"errors"
	"testing"

	"github.com/morse/atlas/server/internal/platform/errs"
)

func ptr[T any](v T) *T { return &v }

func TestDefaultsApplyUntilSomethingIsStored(t *testing.T) {
	s, err := parseSettings("Acme", nil)
	if err != nil || s.NotFoundThreshold != 0.35 || !s.RequireCitation || s.RetentionDays != 730 || s.OrgName != "Acme" {
		t.Fatalf("settings %+v, %v", s, err)
	}
}

func TestStoredValuesOverrideTheDefaultsAndKeepTheRest(t *testing.T) {
	s, _ := parseSettings("Acme", []byte(`{"answer_language":"es","require_citation":false}`))
	if s.AnswerLanguage != "es" || s.RequireCitation || s.RetentionDays != 730 {
		t.Fatalf("settings %+v", s)
	}
}

func TestOnlyChangedSettingsAreReported(t *testing.T) {
	cur := DefaultSettings()
	_, changed, err := applySettings(cur, SettingsPatch{NotFoundThreshold: ptr(0.35), RetentionDays: ptr(365), RequireCitation: ptr(false)})
	if err != nil || len(changed) != 2 || changed[0] != "retention_days" || changed[1] != "require_citation" {
		t.Fatalf("changed %v, %v; the threshold was already 0.35", changed, err)
	}
}

func TestInvalidSettingsAreRefused(t *testing.T) {
	bad := map[string]SettingsPatch{
		"empty name":         {OrgName: ptr("  ")},
		"threshold above 1":  {NotFoundThreshold: ptr(1.5)},
		"negative threshold": {NotFoundThreshold: ptr(-0.1)},
		"unknown language":   {AnswerLanguage: ptr("fr")},
		"retention too low":  {RetentionDays: ptr(7)},
		"retention too high": {RetentionDays: ptr(100000)},
	}
	for name, p := range bad {
		if _, _, err := applySettings(DefaultSettings(), p); !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}
