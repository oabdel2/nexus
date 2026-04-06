#!/usr/bin/env bash
# secret-scan.sh — Scans the codebase for potential secrets and credentials.
# Usage: bash scripts/secret-scan.sh [directory]
# Exit code: 0 if clean, 1 if potential secrets found.

set -euo pipefail

SCAN_DIR="${1:-.}"
FOUND=0

echo "=== Nexus Secret Scanner ==="
echo "Scanning directory: ${SCAN_DIR}"
echo ""

# Patterns to search (case-insensitive where appropriate)
PATTERNS=(
  # Nexus API keys
  'nxs_live_[0-9a-fA-F]{32}'
  'nxs_test_[0-9a-fA-F]{32}'
  # Stripe keys
  'sk_live_[0-9a-zA-Z]{20,}'
  'sk_test_[0-9a-zA-Z]{20,}'
  'pk_live_[0-9a-zA-Z]{20,}'
  'pk_test_[0-9a-zA-Z]{20,}'
  'whsec_[0-9a-zA-Z]{20,}'
  # AWS
  'AKIA[0-9A-Z]{16}'
  'aws_secret_access_key'
  # Generic secrets
  'password\s*[:=]\s*["\x27][^"\x27]{8,}'
  'secret\s*[:=]\s*["\x27][^"\x27]{8,}'
  'api_key\s*[:=]\s*["\x27][^"\x27]{8,}'
  'token\s*[:=]\s*["\x27][^"\x27]{8,}'
  # Private keys
  'BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY'
  # JWT tokens
  'eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}'
  # Generic hex secrets (64+ chars, likely SHA-256 hashes used as secrets)
  'SECRET.*[0-9a-fA-F]{64}'
)

# Files/dirs to exclude
EXCLUDES=(
  "--exclude-dir=.git"
  "--exclude-dir=vendor"
  "--exclude-dir=node_modules"
  "--exclude=*.exe"
  "--exclude=*.dll"
  "--exclude=*.so"
  "--exclude=*.dylib"
  "--exclude=go.sum"
  "--exclude=secret-scan.sh"
  "--exclude=security-audit.md"
  "--exclude=*_test.go"
)

for pattern in "${PATTERNS[@]}"; do
  MATCHES=$(grep -rEn "${EXCLUDES[@]}" "$pattern" "$SCAN_DIR" 2>/dev/null || true)
  if [ -n "$MATCHES" ]; then
    echo "!! POTENTIAL SECRET: pattern '$pattern'"
    echo "$MATCHES" | head -10
    echo ""
    FOUND=1
  fi
done

echo "==========================="
if [ "$FOUND" -eq 1 ]; then
  echo "WARNING: Potential secrets detected! Review the matches above."
  exit 1
else
  echo "CLEAN: No potential secrets found."
  exit 0
fi
