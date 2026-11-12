#!/usr/bin/env bash
# Build locally and register the supported Codex STDIO configuration.
set -euo pipefail
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
register=true
check_only=false
for arg in "$@"; do
  case "$arg" in
    --no-register) register=false ;;
    --check) check_only=true ;;
    -h|--help)
      echo 'Usage: bash scripts/install.sh [--no-register] [--check]'
      echo 'Requires Go, Node.js/npm, make, C compiler/SQLite, and Codex CLI.'
      exit 0 ;;
    *) echo "Unknown option: $arg" >&2; exit 2 ;;
  esac
done
case "$(uname -s)" in
  Darwin|Linux) ;;
  *) echo 'Supported installers: macOS and Linux. Windows: use WSL2.' >&2; exit 1 ;;
esac
for program in go node npm make cc; do
  if ! command -v "$program" >/dev/null 2>&1; then
    echo "Required program not found: $program. See README.md requirements." >&2
    exit 1
  fi
done
if "$register" && ! command -v codex >/dev/null 2>&1; then
  echo 'Codex CLI is required. Install it and sign in, or pass --no-register.' >&2
  exit 1
fi
node -e 'const [major,minor]=process.versions.node.split(".").map(Number); if(major<22 || (major===22 && minor<12)){console.error("Node.js 22.12+ required");process.exit(1)}'
required_go=$(awk '$1 == "go" {print $2; exit}' "$project_dir/go.mod")
echo "Go toolchain required: $required_go or newer (go may download it automatically)."
if "$check_only"; then
  echo 'Programs found. SQLite headers/toolchain are verified during the build.'
  printf 'Registration: codex mcp add ai-workspace -- sh %q\n' "$project_dir/scripts/run-mcp.sh"
  exit 0
fi
cd "$project_dir"
npm ci
make build
if "$register"; then
  codex mcp add ai-workspace -- sh "$project_dir/scripts/run-mcp.sh"
  codex mcp get ai-workspace
  echo 'Installed. Reopen Codex; it starts MCP and the dashboard at http://localhost:8080.'
else
  echo 'Built. Register later: codex mcp add ai-workspace -- sh /absolute/path/scripts/run-mcp.sh'
fi
echo 'Stop any separately running server on port 8080 before Codex starts this MCP.'
echo 'Keep this checkout: the MCP configuration points to its launcher.'
