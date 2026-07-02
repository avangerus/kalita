#!/usr/bin/env bash
# Copy the disk UI into the embed location, then build the single-file box.
set -e
cd "$(dirname "$0")/.."
rm -rf internal/webui/embedded
cp -r web internal/webui/embedded
# packages/kalita-ui is served relative to web/ (../packages/kalita-ui/... from
# web/app.js and web/catalog.html — see cmd/kalita/main.go's /packages/ mount
# for the disk-serving case). The embedded build is one flat FileServer over
# internal/webui/embedded, so the same relative path needs a physical
# packages/kalita-ui directory INSIDE the embedded tree too.
mkdir -p internal/webui/embedded/packages
cp -r packages/kalita-ui internal/webui/embedded/packages/kalita-ui
go build -tags embedui -o bin/kalita.exe ./cmd/kalita
echo "built single-file box: bin/kalita.exe (UI embedded)"
