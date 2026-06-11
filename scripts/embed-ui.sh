#!/usr/bin/env bash
# Copy the disk UI into the embed location, then build the single-file box.
set -e
cd "$(dirname "$0")/.."
rm -rf internal/webui/embedded
cp -r web internal/webui/embedded
go build -tags embedui -o bin/kalita.exe ./cmd/kalita
echo "built single-file box: bin/kalita.exe (UI embedded)"
