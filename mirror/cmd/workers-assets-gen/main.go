// Command workers-assets-gen has moved to github.com/syumai/workers-go.
//
// This stub exists only so that `go install github.com/syumai/workers/cmd/workers-assets-gen@...`
// keeps resolving to something meaningful instead of silently vanishing; it does not implement the
// generator itself, to avoid the embedded runtime assets drifting between this mirror and
// workers-go.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "workers-assets-gen has moved to github.com/syumai/workers-go/cmd/workers-assets-gen; run: go run github.com/syumai/workers-go/cmd/workers-assets-gen ...")
	os.Exit(1)
}
