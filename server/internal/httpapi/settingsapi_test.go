package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/morse/atlas/server/internal/access"
)

type fakeSettings struct {
	fakeAccounts
	stored access.Settings
	patch  access.SettingsPatch
}

func (f *fakeSettings) Settings(context.Context, access.Principal) (access.Settings, error) {
	return f.stored, nil
}

func (f *fakeSettings) UpdateSettings(_ context.Context, _ access.Principal, p access.SettingsPatch) (access.Settings, error) {
	f.patch = p
	return f.stored, nil
}

func settingsServer(f *fakeSettings) *Server {
	f.sessions = people.sessions
	f.stored = access.DefaultSettings()
	f.stored.OrgName = "Acme"
	return &Server{Accounts: f, Deployment: Deployment{ChatModel: "qwen", EmbedModel: "bge-m3", RerankModel: "bge-reranker", NetworkIsolated: true}}
}

func TestSettingsCombineTheOrganizationsChoicesWithTheDeployment(t *testing.T) {
	rec := serveTo(t, settingsServer(&fakeSettings{}), adminRequest(http.MethodGet, "/api/v1/settings", ""))
	for _, want := range []string{`"org_name":"Acme"`, `"chat_model":"qwen"`, `"network_isolated":true`, `"require_citation":true`, `"retention_days":730`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body lacks %s: %s", want, rec.Body)
		}
	}
}

func TestThePatchCarriesOnlyWhatWasSentAndRoundsTheThreshold(t *testing.T) {
	f := &fakeSettings{}
	serveTo(t, settingsServer(f), adminRequest(http.MethodPatch, "/api/v1/settings", `{"not_found_threshold":0.35,"answer_language":"es"}`))
	if f.patch.NotFoundThreshold == nil || *f.patch.NotFoundThreshold != 0.35 || *f.patch.AnswerLanguage != "es" || f.patch.OrgName != nil || f.patch.RetentionDays != nil {
		t.Fatalf("patch = %+v; 0.35 must arrive as exactly 0.35", f.patch)
	}
}

func TestOnlyAdministratorsTouchSettings(t *testing.T) {
	req := adminRequest(http.MethodGet, "/api/v1/settings", "")
	req.Header.Set("Cookie", sessionCookie+"=viewer")
	if rec := serveTo(t, settingsServer(&fakeSettings{}), req); rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}
