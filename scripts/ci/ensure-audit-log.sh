#!/usr/bin/env bash
set -euo pipefail

ensure_with_sudo() {
  local cmd=("$@")
  if sudo "${cmd[@]}"; then
    return 0
  fi
  "${cmd[@]}"
}

ensure_with_sudo mkdir -p /tmp/kind-audit
ensure_with_sudo touch /tmp/kind-audit/audit.log
ensure_with_sudo chmod 0644 /tmp/kind-audit/audit.log
echo "Audit log ready at /tmp/kind-audit/audit.log"
