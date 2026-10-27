#!/bin/zsh
set -eu
source "$(dirname "$0")/env.sh"
[[ -d "dist/Gemma Desktop.app" ]] || { echo "Run scripts/build.sh first"; exit 1; }
codesign --verify --deep --strict "dist/Gemma Desktop.app"
ditto -c -k --sequesterRsrc --keepParent "dist/Gemma Desktop.app" dist/GemmaDesktop-local.zip
shasum -a 256 dist/GemmaDesktop-local.zip > dist/GemmaDesktop-local.zip.sha256
echo "Local package: $GEMMA_ROOT/dist/GemmaDesktop-local.zip"
