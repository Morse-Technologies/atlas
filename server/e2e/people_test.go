//go:build e2e

package e2e

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

const readerPassword = "bea-own-long-password-1"

// addReader creates a person who only asks questions, signs them in with a client of their own and has them
// choose their password, as the first sign-in demands.
func (j *journey) addReader(t *testing.T) *api {
	var created struct {
		Password string `json:"password"`
	}
	person := map[string]any{"name": "Bea", "email": "bea@acme.test", "roles": []string{"viewer"}, "second_factor": false}
	expect(t, j.api.post("/users", person), http.StatusCreated).json(t, &created)
	bea := newAPI(t)
	var challenge struct {
		ChallengeID string `json:"challenge_id"`
	}
	expect(t, bea.post("/auth/login", map[string]string{"email": "bea@acme.test", "password": created.Password}), http.StatusOK).json(t, &challenge)
	expect(t, bea.post("/auth/verify", map[string]string{"challenge_id": challenge.ChallengeID, "code": "000000"}), http.StatusNoContent)
	change := map[string]string{"current_password": created.Password, "new_password": readerPassword}
	expect(t, bea.do(request{method: http.MethodPut, path: "/me/password", body: change}), http.StatusNoContent)
	return bea
}

// documentIDs follows Next-Cursor one document at a time, so the paged list is what is checked.
func documentIDs(t *testing.T, a *api) []string {
	t.Helper()
	var ids []string
	for cursor := ""; ; {
		path := "/documents?limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		page := expect(t, a.get(path), http.StatusOK)
		var docs []docJSON
		page.json(t, &docs)
		for _, d := range docs {
			ids = append(ids, d.ID)
		}
		if cursor = page.header.Get("Next-Cursor"); cursor == "" {
			return ids
		}
	}
}

// twoPeopleSeeOnlyWhatTheyMayRead restricts the folder to the administrator: the second person must not find its
// documents in the paged list, by id, in the related documents, in a search, on the map or in an export.
func (j *journey) twoPeopleSeeOnlyWhatTheyMayRead(t *testing.T) {
	bea := j.addReader(t)
	if !slices.Contains(documentIDs(t, bea), j.golden.ID) {
		t.Fatal("while the folder is open to everyone Bea must see its documents")
	}
	expect(t, j.api.patch("/sources/"+j.sourceID, map[string]any{"access": "restricted", "readers": []string{j.userID}}), http.StatusOK)
	j.requireHiddenFrom(t, bea)
	expect(t, j.api.patch("/sources/"+j.sourceID, map[string]any{"access": "everyone"}), http.StatusOK)
	if !slices.Contains(documentIDs(t, bea), j.golden.ID) {
		t.Fatal("opening the folder again must give Bea its documents back")
	}
}

func (j *journey) requireHiddenFrom(t *testing.T, bea *api) {
	t.Helper()
	if slices.Contains(documentIDs(t, bea), j.golden.ID) {
		t.Error("the paged list shows a document of a folder Bea may not read")
	}
	expect(t, bea.get("/documents/"+j.golden.ID), http.StatusNotFound)
	expect(t, bea.get("/documents/"+j.golden.ID+"/related"), http.StatusNotFound)
	j.requireSearchStaysWithin(t, bea)
	j.requireNotOnTheMap(t, bea)
	expect(t, bea.get("/audit/export?from=2020-01-01&to=2100-01-01"), http.StatusForbidden)
}

// requireSearchStaysWithin: an answer to Bea may cite the uploaded copy of the document, which she may read, but
// nothing she cannot list.
func (j *journey) requireSearchStaysWithin(t *testing.T, bea *api) {
	t.Helper()
	readable := documentIDs(t, bea)
	_, answer := bea.ask("¿Cómo se hace el montaje del husillo?")
	for _, c := range answer.Citations {
		if c.DocumentID == j.golden.ID || !slices.Contains(readable, c.DocumentID) {
			t.Errorf("a search by Bea cites document %s, which she may not read (she can read %v)", c.DocumentID, readable)
		}
	}
}

// requireNotOnTheMap: without a Pro license the map is closed to everybody; with one, it must not centre on the document.
func (j *journey) requireNotOnTheMap(t *testing.T, bea *api) {
	t.Helper()
	r := bea.get("/graph/neighborhood?node_id=" + j.golden.ID)
	if r.status == http.StatusOK && slices.Contains(nodeIDs(t, r), j.golden.ID) {
		t.Error("the map shows a document of a folder Bea may not read")
	}
}

func nodeIDs(t *testing.T, r reply) []string {
	t.Helper()
	var graph struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	r.json(t, &graph)
	ids := make([]string, len(graph.Nodes))
	for i, n := range graph.Nodes {
		ids[i] = n.ID
	}
	return ids
}
