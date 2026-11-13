package httpapi

import (
	"context"
	"math"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/documents"
)

// SearchLanguages is the language of the word search, which the documents module keeps because it indexes with it.
// *documents.Library implements it.
type SearchLanguages interface {
	SearchLanguage(ctx context.Context, who documents.Actor) (string, error)
	SetSearchLanguage(ctx context.Context, who documents.Actor, language string) error
}

// Deployment holds what the installation decides and an administrator cannot change from the screen:
// which models run, and whether the server may reach the internet.
type Deployment struct {
	ChatModel       string
	EmbedModel      string
	RerankModel     string
	NetworkIsolated bool
	AnswerMode      string   // what a question gets with the models installed
	AnswerGaps      []string // why it is not better: one fixed sentence per missing model
}

// thresholdPrecision rounds the threshold to four decimals, so 0.35 sent as a 32-bit float is still 0.35.
const thresholdPrecision = 10000

// GetSettings returns the organization's settings with the deployment's read-only values.
func (s *Server) GetSettings(ctx context.Context, _ GetSettingsRequestObject) (GetSettingsResponseObject, error) {
	cur, err := s.currentSettings(ctx)
	if err != nil {
		return nil, err
	}
	return GetSettings200JSONResponse(cur), nil
}

// UpdateSettings changes the organization's settings.
func (s *Server) UpdateSettings(ctx context.Context, req UpdateSettingsRequestObject) (UpdateSettingsResponseObject, error) {
	if _, err := s.Accounts.UpdateSettings(ctx, principalFrom(ctx), toSettingsPatch(req.Body)); err != nil {
		return nil, err
	}
	if req.Body.SearchLanguage != nil {
		if err := s.SearchLanguages.SetSearchLanguage(ctx, documentActor(ctx), string(*req.Body.SearchLanguage)); err != nil {
			return nil, err
		}
	}
	next, err := s.currentSettings(ctx)
	if err != nil {
		return nil, err
	}
	return UpdateSettings200JSONResponse(next), nil
}

// currentSettings joins what the organization chose (access), the language of its word search (documents) and what
// the deployment decides.
func (s *Server) currentSettings(ctx context.Context) (Settings, error) {
	cur, err := s.Accounts.Settings(ctx, principalFrom(ctx))
	if err != nil {
		return Settings{}, err
	}
	language, err := s.SearchLanguages.SearchLanguage(ctx, documentActor(ctx))
	if err != nil {
		return Settings{}, err
	}
	return s.settingsJSON(cur, SettingsSearchLanguage(language)), nil
}

func (s *Server) settingsJSON(cur access.Settings, searchLanguage SettingsSearchLanguage) Settings {
	return Settings{OrgName: cur.OrgName, ChatModel: s.Deployment.ChatModel, EmbedModel: s.Deployment.EmbedModel,
		RerankModel: s.Deployment.RerankModel, NetworkIsolated: s.Deployment.NetworkIsolated,
		RequireCitation: cur.RequireCitation, WarnContradictions: cur.WarnContradictions,
		NotFoundThreshold: float32(cur.NotFoundThreshold), AnswerLanguage: SettingsAnswerLanguage(cur.AnswerLanguage),
		SearchLanguage: searchLanguage, RetentionDays: cur.RetentionDays, AnswerMode: SettingsAnswerMode(s.Deployment.AnswerMode), AnswerGaps: s.Deployment.AnswerGaps}
}

func toSettingsPatch(b *SettingsPatch) access.SettingsPatch {
	p := access.SettingsPatch{OrgName: b.OrgName, RetentionDays: b.RetentionDays, RequireCitation: b.RequireCitation,
		WarnContradictions: b.WarnContradictions}
	if b.NotFoundThreshold != nil {
		rounded := math.Round(float64(*b.NotFoundThreshold)*thresholdPrecision) / thresholdPrecision
		p.NotFoundThreshold = &rounded
	}
	if b.AnswerLanguage != nil {
		lang := string(*b.AnswerLanguage)
		p.AnswerLanguage = &lang
	}
	return p
}
