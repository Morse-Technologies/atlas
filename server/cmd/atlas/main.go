// Command atlas is the single Atlas binary; the first argument picks the role (ATLAS.md §4.1).
package main

import (
	"os"

	"github.com/morse/atlas/server/app"
)

func main() {
	os.Exit(app.Main(os.Args[1:]))
}
