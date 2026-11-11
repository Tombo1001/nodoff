#!/usr/bin/env bash
# Build dist/nodoff.exe for 64-bit Windows from any OS with Go installed. No cgo, no C toolchain.
set -euo pipefail
cd "$(dirname "$0")/.."
go vet ./...
GOOS=windows go vet ./...
go test ./...
go run ./tools/mkrsrc
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags "-s -w -H windowsgui -buildid=" -o dist/nodoff.exe .
sha256sum dist/nodoff.exe
