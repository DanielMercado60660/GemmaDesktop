#!/bin/zsh
set -eu
source "$(dirname "$0")/env.sh"
: "${GEMMA_LIVE_MODEL:?Select an installed local model, e.g. gemma4:12b-mlx}"
go test ./internal/engine -run TestLiveOllama -count=1 -v
