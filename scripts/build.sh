#!/bin/zsh
set -eu
source "$(dirname "$0")/env.sh"
go mod download
(cd frontend && npm ci && npm run build)
mkdir -p "dist/Gemma Desktop.app/Contents/MacOS" "dist/Gemma Desktop.app/Contents/Resources"
python3 scripts/package_harness.py
python3 scripts/build-info.py capture
go build -tags production -trimpath -ldflags='-s -w' -o "dist/Gemma Desktop.app/Contents/MacOS/gemma-desktop" .
cp packaging/Info.plist "dist/Gemma Desktop.app/Contents/Info.plist"
if [[ -f packaging/AppIcon.icns ]]; then cp packaging/AppIcon.icns "dist/Gemma Desktop.app/Contents/Resources/AppIcon.icns"; fi
codesign --force --sign - "dist/Gemma Desktop.app"
codesign --verify --deep --strict "dist/Gemma Desktop.app"
python3 scripts/build-info.py verify
echo "Built: $GEMMA_ROOT/dist/Gemma Desktop.app"
