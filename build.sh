#!/bin/sh
# Build the app and give it an ad-hoc signature; macOS on Apple Silicon
# reports unsigned apps as "damaged".
set -e
cd "$(dirname "$0")"
PATH="$PATH:$(go env GOPATH)/bin" wails build -clean
codesign --force --deep --sign - build/bin/kikoku2002.app
codesign --verify --deep --strict build/bin/kikoku2002.app
echo "built build/bin/kikoku2002.app"
