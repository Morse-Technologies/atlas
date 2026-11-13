package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/documents"
)

// fakeLanguages keeps the word-search language in memory.
type fakeLanguages struct{ language string }

func (f *fakeLanguages) SearchLanguage(context.Context, documents.Actor) (string, error) {
	if f.language == "" {
		return "none", nil
	}
	return f.language, nil
}

func (f *fakeLanguages) SetSearchLanguage(_ context.Context, _ documents.Actor, language string) error {
	f.language = language
	return nil
}

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
	return &Server{Accounts: f, SearchLanguages: &fakeLanguages{}, Deployment: Deployment{ChatModel: "qwen", EmbedModel: "bge-m3", RerankModel: "bge-reranker", NetworkIsolated: true,
		AnswerMode: "passages", AnswerGaps: []string{"No reranker: answers are not written"}}}
}

func TestSettingsCombineTheOrganizationsChoicesWithTheDeployment(t *testing.T) {
	rec := serveTo(t, settingsServer(&fakeSettings{}), adminRequest(http.MethodGet, "/api/v1/settings", ""))
	for _, want := range []string{`"org_name":"Acme"`, `"chat_model":"qwen"`, `"network_isolated":true`, `"require_citation":true`, `"retention_days":730`,
		`"answer_mode":"passages"`, `"answer_gaps":["No reranker: answers are not written"]`} {
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

func TestTheWordSearchLanguageIsPartOfTheSettingsAndIsChangedThroughDocuments(t *testing.T) {
	server := settingsServer(&fakeSettings{})
	if rec := serveTo(t, server, adminRequest(http.MethodGet, "/api/v1/settings", "")); !strings.Contains(rec.Body.String(), `"search_language":"none"`) {
		t.Fatalf("the default language is missing: %s", rec.Body)
	}
	rec := serveTo(t, server, adminRequest(http.MethodPatch, "/api/v1/settings", `{"search_language":"es"}`))
	if !strings.Contains(rec.Body.String(), `"search_language":"es"`) || server.SearchLanguages.(*fakeLanguages).language != "es" {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
}

func TestAPatchWithoutALanguageLeavesTheLanguageAlone(t *testing.T) {
	server := settingsServer(&fakeSettings{})
	languages := server.SearchLanguages.(*fakeLanguages)
	languages.language = "en"
	serveTo(t, server, adminRequest(http.MethodPatch, "/api/v1/settings", `{"retention_days":365}`))
	if languages.language != "en" {
		t.Fatalf("language = %q after a patch that did not mention it", languages.language)
	}
}

func TestOnlyAdministratorsTouchSettings(t *testing.T) {
	req := adminRequest(http.MethodGet, "/api/v1/settings", "")
	req.Header.Set("Cookie", sessionCookie+"=viewer")
	if rec := serveTo(t, settingsServer(&fakeSettings{}), req); rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}
