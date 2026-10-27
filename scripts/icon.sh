#!/bin/zsh
set -eu
source "$(dirname "$0")/env.sh"
mkdir -p .cache/AppIcon.iconset
swift -module-cache-path .cache/swift packaging/icon.swift .cache/AppIcon.png
for size in 16 32 128 256 512; do
 sips -z "$size" "$size" .cache/AppIcon.png --out ".cache/AppIcon.iconset/icon_${size}x${size}.png" >/dev/null
 double=$((size * 2))
 sips -z "$double" "$double" .cache/AppIcon.png --out ".cache/AppIcon.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns .cache/AppIcon.iconset -o packaging/AppIcon.icns
