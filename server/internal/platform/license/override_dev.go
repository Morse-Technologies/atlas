//go:build devlicense

package license

import (
	"os"
	"strings"
)

// developmentKey is the key of a throwaway development license (deploy/scripts/dev-env.sh), from the environment.
func developmentKey() string { return strings.TrimSpace(os.Getenv("ATLAS_LICENSE_PUBLIC_KEY")) }
