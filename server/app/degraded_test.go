package app

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/morse/atlas/server/internal/httpapi"
)

// logged runs fn with the default logger writing to a buffer.
func logged(fn func()) string {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var out bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	fn()
	return out.String()
}

func TestAStartWithoutWrittenAnswersWarnsWithTheReasons(t *testing.T) {
	deployment := httpapi.Deployment{AnswerMode: "keyword", AnswerGaps: []string{"No language model: answers are not written"}}

	got := logged(func() { warnIfAnswersAreDegraded(deployment) })

	for _, want := range []string{"level=WARN", "answers are degraded", "mode=keyword", "No language model"} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q lacks %q", got, want)
		}
	}
}

func TestAStartWithWrittenAnswersSaysNothing(t *testing.T) {
	if got := logged(func() { warnIfAnswersAreDegraded(httpapi.Deployment{AnswerMode: "generated"}) }); got != "" {
		t.Fatalf("log %q, want none", got)
	}
}
