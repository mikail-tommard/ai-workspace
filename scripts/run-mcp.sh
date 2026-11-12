#!/bin/sh
# Keep MCP stdout exclusively for protocol messages.
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ ! -x "$project_dir/ai-workspace" ]; then
  echo "ai-workspace is not built. Run: bash scripts/install.sh" >&2
  exit 1
fi
case "$(uname -s)" in
  Darwin) default_data="$HOME/Library/Application Support/ai-workspace" ;;
  *) default_data="${XDG_DATA_HOME:-$HOME/.local/share}/ai-workspace" ;;
esac
cd "$project_dir"
exec "$project_dir/ai-workspace" --transport stdio \
  --data-dir "${WORKSPACE_DATA_DIR:-$default_data}" \
  --addr "${WORKSPACE_ADDR:-127.0.0.1:8080}" "$@"
