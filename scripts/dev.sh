#!/bin/zsh
set -eu
source "$(dirname "$0")/env.sh"
(cd frontend && npm run build)
go run . --serve 127.0.0.1:8787 --data-dir "$GEMMA_ROOT/.runtime/dev"
