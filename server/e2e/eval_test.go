//go:build e2e

package e2e

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// execInAPIEnv is the command that runs a program in the api container (deploy/scripts/e2e.sh sets it).
// shortcut: it is split on spaces, so the repository path cannot contain any; when it must, pass the words as a list.
const execInAPIEnv = "ATLAS_E2E_API_EXEC"

// giveEveryCopyTheCode names the three copies of the golden PDF (the original, copia.pdf and the upload) by one
// code. The answer cites whichever copy ranks first, so a question can only expect the family, not one file.
func (j *journey) giveEveryCopyTheCode(t *testing.T, code string) {
	t.Helper()
	for _, d := range j.indexed() {
		expect(t, j.api.patch("/documents/"+d.ID+"/metadata", map[string]string{"code": code}), http.StatusOK)
	}
}

// evalMeasuresTheAnswers runs `atlas eval` in the container over the sample questions: the fake models must
// reach the bar, the question outside the documents must come back "not found", and nothing is stored.
func (j *journey) evalMeasuresTheAnswers(t *testing.T) {
	prefix := strings.Fields(os.Getenv(execInAPIEnv))
	if len(prefix) == 0 {
		t.Skipf("%s is not set: deploy/scripts/e2e.sh sets it", execInAPIEnv)
	}
	j.giveEveryCopyTheCode(t, "PNT-014")
	args := append(prefix[1:], "/atlas", "eval", "--file", "/testdata/eval/sample.jsonl", "--as", adminEmail, "--json")
	out, err := exec.Command(prefix[0], args...).Output()
	var failed *exec.ExitError
	if errors.As(err, &failed) {
		t.Fatalf("atlas eval failed: %v\n%s\n%s", err, out, failed.Stderr)
	}
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Precision float64 `json:"precision"`
		Rows      []struct{ Question, Verdict string }
	}
	if err := json.Unmarshal(out, &report); err != nil || len(report.Rows) != 6 || report.Rows[5].Verdict != "rightly_not_found" {
		t.Fatalf("report = %s (%v)", out, err)
	}
	var list []struct{ ID string }
	expect(t, j.api.get("/conversations"), http.StatusOK).json(t, &list)
	if len(list) != 2 {
		t.Fatalf("conversations = %d after the evaluation, want the same 2: an evaluation stores nothing", len(list))
	}
}
