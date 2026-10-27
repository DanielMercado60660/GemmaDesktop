#!/bin/zsh
set -eu
GEMMA_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export GOPATH="$GEMMA_ROOT/.cache/go"
export GOCACHE="$GEMMA_ROOT/.cache/gobuild"
export GOTOOLCHAIN=go1.26.10
export GOPROXY=https://proxy.golang.org
export CGO_ENABLED=1
export MACOSX_DEPLOYMENT_TARGET=14.0
export CGO_CFLAGS="-mmacosx-version-min=14.0"
export CGO_LDFLAGS="-mmacosx-version-min=14.0"
cd "$GEMMA_ROOT"
