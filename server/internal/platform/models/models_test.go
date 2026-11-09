package models

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/morse/atlas/server/internal/platform/errs"
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
