//go:build !devlicense

package license

// developmentKey is always empty in a normal build: the environment cannot replace Morse's key.
func developmentKey() string { return "" }
