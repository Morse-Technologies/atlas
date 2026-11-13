package models

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/metrics"
)

type statusCase struct {
	class  errs.Class
	reason errs.Reason
}

var statusCases = map[int]statusCase{
	http.StatusOK:                    {class: errs.ClassUnknown},
	http.StatusRequestTimeout:        {class: errs.ClassTransient},
	http.StatusTooManyRequests:       {class: errs.ClassTransient},
	http.StatusServiceUnavailable:    {class: errs.ClassTransient},
	http.StatusRequestEntityTooLarge: {class: errs.ClassPermanent, reason: errs.ReasonTooLarge},
	http.StatusUnprocessableEntity:   {class: errs.ClassPermanent, reason: errs.ReasonCorrupt},
}

func TestStatusErrorClassifiesResponses(t *testing.T) {
	for code, want := range statusCases {
		resp := &http.Response{StatusCode: code, Request: httptest.NewRequest(http.MethodPost, "/convert", nil)}
		err := StatusError(resp)
		if errs.Classify(err) != want.class || (want.reason != "" && errs.PermanentReason(err) != want.reason) {
			t.Errorf("status %d: got %v, want class %d %s", code, err, want.class, want.reason)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMeasuredCountsCallsOfTheModel(t *testing.T) {
	client := &http.Client{Transport: Measured("testmodel", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: r}, nil
	}))}
	resp, err := client.Get("http://model.invalid/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	rec := httptest.NewRecorder()
	metrics.Default.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if want := `atlas_model_request_seconds_count{model="testmodel"} 1`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("missing %q in %q", want, rec.Body.String())
	}
}
