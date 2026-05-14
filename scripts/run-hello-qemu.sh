#!/usr/bin/env bash
set -euo pipefail

mkdir -p build
go run ./cmd/wrela build --mode dev examples/hello/main.wrela -o build/hello.efi
go test ./tests/e2e -run TestHelloQEMU -v
