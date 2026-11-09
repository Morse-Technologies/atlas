package httpapi

import (
	"context"
	"math"

	"github.com/morse/atlas/server/internal/access"
)

// Deployment holds what the installation decides and an administrator cannot change from the screen:
// which models run, and whether the server may reach the internet.
type Deployment struct {
	ChatModel       string
	EmbedModel      string
	RerankModel     string
	NetworkIsolated bool
}

// thresholdPrecision rounds the threshold to four decimals, so 0.35 sent as a 32-bit float is still 0.35.
const thresholdPrecision = 10000

// GetSettings returns the organization's settings with the deployment's read-only values.
func (s *Server) GetSettings(ctx context.Context, _ GetSettingsRequestObject) (GetSettingsResponseObject, error) {
	cur, err := s.Accounts.Settings(ctx, principalFrom(ctx))
	if err != nil {
		return nil, err
	}
	return GetSettings200JSONResponse(s.settingsJSON(cur)), nil
}

// UpdateSettings changes the organization's settings.
func (s *Server) UpdateSettings(ctx context.Context, req UpdateSettingsRequestObject) (UpdateSettingsResponseObject, error) {
	next, err := s.Accounts.UpdateSettings(ctx, principalFrom(ctx), toSettingsPatch(req.Body))
	if err != nil {
		return nil, err
	}
	return UpdateSettings200JSONResponse(s.settingsJSON(next)), nil
}

func (s *Server) settingsJSON(cur access.Settings) Settings {
	return Settings{OrgName: cur.OrgName, ChatModel: s.Deployment.ChatModel, EmbedModel: s.Deployment.EmbedModel,
		RerankModel: s.Deployment.RerankModel, NetworkIsolated: s.Deployment.NetworkIsolated,
		RequireCitation: cur.RequireCitation, WarnContradictions: cur.WarnContradictions,
		NotFoundThreshold: float32(cur.NotFoundThreshold), AnswerLanguage: SettingsAnswerLanguage(cur.AnswerLanguage),
		RetentionDays: cur.RetentionDays}
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
