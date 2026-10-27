#!/bin/zsh
set -eu
source "$(dirname "$0")/env.sh"
go test -race ./internal/...
(cd frontend && npm test && npm run build)
