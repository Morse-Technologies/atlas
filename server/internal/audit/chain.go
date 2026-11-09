package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// hashHexLen is the length of a SHA-256 hash in hex.
const hashHexLen = 64

// GenesisHash is the prev_hash of the first event of a chain.
var GenesisHash = strings.Repeat("0", hashHexLen)

// canonical fixes the fields and their order that the hash covers. json.Marshal of a struct is deterministic,
// and there is no map in it (ATLAS.md §15).
type canonical struct {
	ID      int64      `json:"id"`
	At      string     `json:"at"`
	ActorID *uuid.UUID `json:"actor_id"`
	Action  string     `json:"action"`
	Subject string     `json:"subject"`
	Detail  string     `json:"detail"`
}

// hashOf is SHA-256 over prevHash followed by the canonical form of the event.
func hashOf(prevHash string, e Event) (string, error) {
	body, err := json.Marshal(canonical{
		ID: e.ID, At: e.At.UTC().Format(time.RFC3339Nano), ActorID: e.ActorID,
		Action: e.Action, Subject: e.Subject, Detail: e.Detail,
	})
	if err != nil {
		return "", fmt.Errorf("audit: canonical form: %w", err)
	}
	sum := sha256.Sum256(append([]byte(prevHash), body...))
	return hex.EncodeToString(sum[:]), nil
}

// linkIsValid reports whether e follows prevHash and carries the hash its content produces.
func linkIsValid(prevHash string, e Event) bool {
	want, err := hashOf(prevHash, e)
	return err == nil && e.PrevHash == prevHash && e.Hash == want
}
