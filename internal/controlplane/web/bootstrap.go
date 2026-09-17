package web

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Bootstrap script template served at GET /bootstrap/node.sh.
// Placeholders __PANEL_HOST__ and __GRPC_HOST__ are filled from the request Host.
const bootstrapScriptTemplate = `#!/usr/bin/env bash
# Simple-VPN-Builder node bootstrap (served by the control plane).
# Pre-create a node with the same name in the panel first
# (dashboard Nodes -> Add Node, or POST /api/v1/nodes) — unknown nodes are refused.
set -euo pipefail

REPO_OWNER="ivanchik-byte"
REPO_NAME="Simple-VPN-Builder"
REF="0.1.0v"
TOKEN=""
PANEL="http://__PANEL_HOST__:8110"
GRPC="__GRPC_HOST__:9090"
NODE_NAME="$(hostname)"

log() { echo "[bootstrap] $*"; }
die() { echo "[bootstrap] ERROR: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --token) shift; TOKEN="$1" ;;
        --panel) shift; PANEL="$1" ;;
        --grpc) shift; GRPC="$1" ;;
        --node-name) shift; NODE_NAME="$1" ;;
        --version) shift; REF="$1" ;;
        -h|--help)
            echo "Usage: node.sh [--token T] [--panel URL] [--grpc host:port] [--node-name NAME] [--version REF]"
            exit 0
            ;;
        *) die "Unknown argument: $1" ;;
    esac
    shift
done

[ "$(id -u)" -eq 0 ] || die "run as root (e.g. via sudo)."
[ -n "$GRPC" ] || die "--grpc host:port is required."

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

installer="$tmp_dir/install.sh"
if ! curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/${REF}/scripts/install.sh" -o "$installer"; then
    log "WARN: ref ${REF} not found, falling back to master."
    curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/master/scripts/install.sh" -o "$installer" \
        || die "could not download the installer."
fi

log "Installing node agent (control plane ${GRPC}, node ${NODE_NAME})..."
bash "$installer" --agent --cp-url "$GRPC" --non-interactive

cfg="/etc/vpnbuilder/agent.yaml"
if [ -n "$NODE_NAME" ] && [ -f "$cfg" ]; then
    sed -i -E "s|^([[:space:]]*node_name:).*|\\1 \"$NODE_NAME\"|" "$cfg" || true
fi
if [ -n "$TOKEN" ] && [ -f "$cfg" ]; then
    grep -q "enrollment_token" "$cfg" || echo "# enrollment_token: \"${TOKEN}\" (reserved; node must be pre-created in panel)" >> "$cfg"
    log "WARN: token-based auto-enrollment is not supported yet — the node must already exist in the panel."
fi

log "Done. Start with: systemctl start vpnbuilder-agent (certs: place mTLS files if your panel requires them)."
log "Panel: $PANEL | gRPC: $GRPC | node: $NODE_NAME"
`

// BootstrapNodeScript serves the node bootstrap installer over HTTP.
// It is intentionally public: it contains no secrets, only install logic.
func BootstrapNodeScript(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	panelHost := host
	grpcHost := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		panelHost = h
		grpcHost = h
	} else if i := strings.LastIndex(host, ":"); i != -1 {
		panelHost = host[:i]
		grpcHost = host[:i]
	}
	script := strings.ReplaceAll(bootstrapScriptTemplate, "__PANEL_HOST__", panelHost+":8110")
	script = strings.ReplaceAll(script, "__GRPC_HOST__", grpcHost)
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, script)
}
