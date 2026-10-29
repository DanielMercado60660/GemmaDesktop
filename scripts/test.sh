#!/bin/zsh
set -eu
source "$(dirname "$0")/env.sh"
go test -race ./internal/...
python3 -m unittest discover -s scripts -p 'test_*.py'
(cd frontend && npm test && npm run build)
