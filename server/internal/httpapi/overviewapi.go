package httpapi

import (
	"context"
	"time"

	"github.com/morse/atlas/server/internal/overview"
	"github.com/morse/atlas/server/internal/platform/db"
)

// PanelSource is what the HTTP layer needs from the overview module. *overview.Service implements it.
type PanelSource interface {
	Panel(ctx context.Context, scope db.Scope) (overview.Panel, error)
}

type overviewJSON struct {
	Today struct {
		Questions       int `json:"questions"`
		Answered        int `json:"answered"`
		NotFound        int `json:"not_found"`
		MedianLatencyMs int `json:"median_latency_ms"`
	} `json:"today"`
	Week      []weekDayJSON `json:"week"`
	TopTopics []struct{}    `json:"top_topics"`
	Documents struct {
		Indexed     int            `json:"indexed"`
		ByBand      map[string]int `json:"by_band"`
		Processing  int            `json:"processing"`
		Quarantined int            `json:"quarantined"`
	} `json:"documents"`
	OpenAlerts struct {
		High   int `json:"high"`
		Medium int `json:"medium"`
		Low    int `json:"low"`
	} `json:"open_alerts"`
	Finding string `json:"finding"`
}

type weekDayJSON struct {
	Day       string `json:"day"`
	Questions int    `json:"questions"`
	NotFound  int    `json:"not_found"`
}

// GetOverview returns the numbers of the panel. Top topics stay empty until questions are classified by topic.
func (s *Server) GetOverview(ctx context.Context, _ GetOverviewRequestObject) (GetOverviewResponseObject, error) {
	p, err := s.Panel.Panel(ctx, db.Scope{Tenant: principalFrom(ctx).TenantID, User: principalFrom(ctx).UserID})
	if err != nil {
		return nil, err
	}
	out, err := viaJSON[Overview](panelJSON(p))
	return GetOverview200JSONResponse(out), err
}

func panelJSON(p overview.Panel) overviewJSON {
	var o overviewJSON
	o.Today.Questions, o.Today.Answered, o.Today.NotFound, o.Today.MedianLatencyMs = p.Today.Questions, p.Today.Answered, p.Today.NotFound, p.Today.MedianLatencyMs
	o.Week = make([]weekDayJSON, len(p.Week))
	for i, d := range p.Week {
		o.Week[i] = weekDayJSON{Day: d.Day.Format(time.DateOnly), Questions: d.Questions, NotFound: d.NotFound}
	}
	o.TopTopics = []struct{}{}
	o.Documents.Indexed, o.Documents.ByBand, o.Documents.Processing, o.Documents.Quarantined = p.Documents.Indexed, p.Documents.ByBand, p.Documents.Processing, p.Documents.Quarantined
	o.OpenAlerts.High, o.OpenAlerts.Medium, o.OpenAlerts.Low = p.OpenAlerts["high"], p.OpenAlerts["medium"], p.OpenAlerts["low"]
	o.Finding = p.Finding
	return o
}
