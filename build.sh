#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-dev}"
OUT_DIR="dist"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

echo "Building VietCoffee $VERSION"

GOOS=linux GOARCH=amd64 go build -o "$OUT_DIR/viet_coffee-linux-amd64" viet_coffee.go
echo "  built $OUT_DIR/viet_coffee-linux-amd64"

GOOS=linux GOARCH=arm64 go build -o "$OUT_DIR/viet_coffee-linux-arm64" viet_coffee.go
echo "  built $OUT_DIR/viet_coffee-linux-arm64"

echo "Done. Binaries in $OUT_DIR/"
