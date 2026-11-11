//go:build !windows

package main

import (
	"fmt"
	"os"
)

// nodoff only runs on Windows. This stub lets `go vet ./...` and the tests run on any OS.
func main() {
	fmt.Fprintln(os.Stderr, "nodoff "+version+" runs on Windows only")
	os.Exit(1)
}
