#!/bin/zsh
set -eu
source "$(dirname "$0")/env.sh"
build_mode=full-installer
if [[ $# -eq 1 && "$1" == --ollama-only ]]; then
  build_mode=ollama-only
elif [[ $# -ne 0 ]]; then
  echo "Usage: ./scripts/build.sh [--ollama-only]" >&2
  exit 2
fi
# A failed dependency, packaging or signing step must not leave a prior build
# manifest looking like the result of this invocation.
rm -f dist/build-info.json dist/build-inputs.json
go mod download
(cd frontend && npm ci && npm run build)
mkdir -p "dist/Gemma Desktop.app/Contents/MacOS" "dist/Gemma Desktop.app/Contents/Resources"
python3 scripts/package_harness.py
python3 scripts/runtime-package.py --build-mode "$build_mode"
python3 scripts/build-info.py capture --build-mode "$build_mode"
go build -tags production -trimpath -ldflags='-s -w' -o "dist/Gemma Desktop.app/Contents/MacOS/gemma-desktop" .
cp packaging/Info.plist "dist/Gemma Desktop.app/Contents/Info.plist"
if [[ -f packaging/AppIcon.icns ]]; then cp packaging/AppIcon.icns "dist/Gemma Desktop.app/Contents/Resources/AppIcon.icns"; fi
codesign --force --sign - "dist/Gemma Desktop.app"
codesign --verify --deep --strict "dist/Gemma Desktop.app"
python3 scripts/build-info.py verify --build-mode "$build_mode"
echo "Built ($build_mode): $GEMMA_ROOT/dist/Gemma Desktop.app"
