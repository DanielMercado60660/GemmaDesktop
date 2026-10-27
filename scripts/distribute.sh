#!/bin/zsh
set -eu
source "$(dirname "$0")/env.sh"
: "${GEMMA_SIGNING_IDENTITY:?Set GEMMA_SIGNING_IDENTITY to a Developer ID Application identity}"
: "${GEMMA_NOTARY_PROFILE:?Set GEMMA_NOTARY_PROFILE to an existing notarytool keychain profile}"
[[ -d "dist/Gemma Desktop.app" ]] || { echo "Run scripts/build.sh first"; exit 1; }
codesign --force --options runtime --timestamp --sign "$GEMMA_SIGNING_IDENTITY" "dist/Gemma Desktop.app"
codesign --verify --deep --strict "dist/Gemma Desktop.app"
ditto -c -k --keepParent "dist/Gemma Desktop.app" dist/GemmaDesktop-notarize.zip
xcrun notarytool submit dist/GemmaDesktop-notarize.zip --keychain-profile "$GEMMA_NOTARY_PROFILE" --wait
xcrun stapler staple "dist/Gemma Desktop.app"
hdiutil create -volname "Gemma Desktop" -srcfolder "dist/Gemma Desktop.app" -ov -format UDZO dist/GemmaDesktop.dmg
codesign --timestamp --sign "$GEMMA_SIGNING_IDENTITY" dist/GemmaDesktop.dmg
echo "Distribution artifact: dist/GemmaDesktop.dmg"
