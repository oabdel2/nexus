#!/usr/bin/env bash
#
# enterprise-init.sh — Bootstrap and launch the Nexus Enterprise stack.
#
# Usage:
#   ./scripts/enterprise-init.sh        # full start
#   ./scripts/enterprise-init.sh down   # tear down
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.enterprise.yml"
CERT_DIR="$PROJECT_ROOT/certs"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $*"; exit 1; }

# ── Tear-down shortcut ─────────────────────────────────────────────────
if [[ "${1:-}" == "down" ]]; then
  info "Tearing down enterprise stack..."
  docker compose -f "$COMPOSE_FILE" down -v
  ok "Stack stopped and volumes removed."
  exit 0
fi

echo ""
echo -e "${CYAN}╔════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║     Nexus Enterprise Stack — Initializer      ║${NC}"
echo -e "${CYAN}╚════════════════════════════════════════════════╝${NC}"
echo ""

# ── 1. Validate prerequisites ──────────────────────────────────────────
info "Checking prerequisites..."

if ! command -v docker &>/dev/null; then
  fail "Docker is not installed. Please install Docker: https://docs.docker.com/get-docker/"
fi
ok "Docker $(docker --version | grep -oP '[\d.]+'| head -1) found"

if docker compose version &>/dev/null; then
  COMPOSE_CMD="docker compose"
  ok "Docker Compose (plugin) found"
elif command -v docker-compose &>/dev/null; then
  COMPOSE_CMD="docker-compose"
  ok "docker-compose $(docker-compose --version | grep -oP '[\d.]+' | head -1) found"
else
  fail "Docker Compose is not installed. Please install it: https://docs.docker.com/compose/install/"
fi

# ── 2. Create required directories ────────────────────────────────────
info "Creating required directories..."
mkdir -p "$CERT_DIR"
mkdir -p "$PROJECT_ROOT/data"
ok "Directories ready"

# ── 3. Generate self-signed TLS certs (dev only) ─────────────────────
if [[ ! -f "$CERT_DIR/server.crt" || ! -f "$CERT_DIR/server.key" ]]; then
  info "Generating self-signed TLS certificates for development..."
  if ! command -v openssl &>/dev/null; then
    warn "openssl not found — skipping TLS cert generation."
    warn "TLS is enabled in config; provide certs in $CERT_DIR or disable TLS."
  else
    openssl req -x509 -newkey rsa:4096 \
      -keyout "$CERT_DIR/server.key" \
      -out "$CERT_DIR/server.crt" \
      -sha256 -days 365 -nodes \
      -subj "/C=US/ST=Dev/L=Local/O=Nexus/OU=Enterprise/CN=localhost" \
      -addext "subjectAltName=DNS:localhost,DNS:nexus,IP:127.0.0.1" \
      2>/dev/null
    chmod 644 "$CERT_DIR/server.crt"
    chmod 600 "$CERT_DIR/server.key"
    ok "Self-signed certificates created in $CERT_DIR"
  fi
else
  ok "TLS certificates already exist in $CERT_DIR"
fi

# ── 4. Build and start the enterprise stack ───────────────────────────
info "Building and starting the enterprise stack..."
$COMPOSE_CMD -f "$COMPOSE_FILE" build --quiet
$COMPOSE_CMD -f "$COMPOSE_FILE" up -d

# ── 5. Wait for all services to become healthy ───────────────────────
SERVICES=(nexus-qdrant nexus-redis nexus-ollama nexus-gateway nexus-prometheus nexus-grafana)
TIMEOUT=180
ELAPSED=0
INTERVAL=5

info "Waiting for all services to become healthy (timeout: ${TIMEOUT}s)..."

all_healthy() {
  for svc in "${SERVICES[@]}"; do
    status=$(docker inspect --format='{{.State.Health.Status}}' "$svc" 2>/dev/null || echo "missing")
    if [[ "$status" != "healthy" ]]; then
      return 1
    fi
  done
  return 0
}

while ! all_healthy; do
  if (( ELAPSED >= TIMEOUT )); then
    warn "Timed out after ${TIMEOUT}s. Some services may still be starting."
    break
  fi
  sleep "$INTERVAL"
  ELAPSED=$((ELAPSED + INTERVAL))
done

# ── 6. Print status summary ──────────────────────────────────────────
echo ""
echo -e "${CYAN}──────────────────────────────────────────────────${NC}"
echo -e "${CYAN}  Service Status Summary${NC}"
echo -e "${CYAN}──────────────────────────────────────────────────${NC}"

printf "  %-20s %-12s %s\n" "SERVICE" "STATUS" "URL"
printf "  %-20s %-12s %s\n" "───────" "──────" "───"

for svc in "${SERVICES[@]}"; do
  status=$(docker inspect --format='{{.State.Health.Status}}' "$svc" 2>/dev/null || echo "not found")
  case "$status" in
    healthy)   color="$GREEN" ;;
    unhealthy) color="$RED" ;;
    *)         color="$YELLOW" ;;
  esac

  case "$svc" in
    nexus-gateway)    url="http://localhost:8080" ;;
    nexus-qdrant)     url="http://localhost:6333" ;;
    nexus-redis)      url="localhost:6379" ;;
    nexus-ollama)     url="http://localhost:11434" ;;
    nexus-prometheus)  url="http://localhost:9090" ;;
    nexus-grafana)    url="http://localhost:3000" ;;
    *)                url="" ;;
  esac

  printf "  %-20s ${color}%-12s${NC} %s\n" "$svc" "$status" "$url"
done

echo ""
echo -e "${CYAN}──────────────────────────────────────────────────${NC}"
echo -e "  Grafana credentials:  admin / ${GRAFANA_ADMIN_PASSWORD:-nexus}"
echo -e "${CYAN}──────────────────────────────────────────────────${NC}"
echo ""

if all_healthy; then
  ok "All services healthy. Nexus Enterprise is ready!"
else
  warn "Some services are not yet healthy. Check: docker compose -f docker-compose.enterprise.yml ps"
fi
