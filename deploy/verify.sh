#!/usr/bin/env bash
# Throwaway end-to-end check of the Docker deployment. Run it on the Mac mini BEFORE the real deployment:
#   deploy/verify.sh            prints PASS/FAIL per check, exits non-zero on any FAIL, cleans up after itself
#   deploy/verify.sh --keep     leave the throwaway stack running for poking around (clean up yourself: see the last line)
# It uses only its own names (compose project calendar-verify, images calendard:verify and calendard:verify2, a random
# free loopback port, fake secrets in a temp dir). It never touches deploy/secrets, deploy/calendard.env, your real
# stack, or port 8787. Needs: docker (with compose v2), curl, python3 (only to find a free port).
set -u

KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

REPO=$(cd "$(dirname "$0")/.." && pwd)
PROJECT=calendar-verify
IMG1=calendard:verify
IMG2=calendard:verify2
TMP=$(mktemp -d "${TMPDIR:-/tmp}/calendar-verify.XXXXXX")
pass=0; fail=0
ok()  { printf '  PASS  %s\n' "$*"; pass=$((pass + 1)); }
bad() { printf '  FAIL  %s\n' "$*"; fail=$((fail + 1)); }
check() { # check "name" cmd...
	local name=$1; shift
	if "$@" >"$TMP/out" 2>&1; then ok "$name"; else bad "$name"; sed 's/^/        /' "$TMP/out" | head -15; fi
}

command -v docker >/dev/null 2>&1 || { echo "docker not found"; exit 2; }
docker info >/dev/null 2>&1 || { echo "docker daemon not reachable (start OrbStack / Docker Desktop)"; exit 2; }
docker compose version >/dev/null 2>&1 || { echo "docker compose v2 missing"; exit 2; }
command -v curl >/dev/null 2>&1 || { echo "curl not found"; exit 2; }

# free loopback port (never 8787)
PORT=$(python3 - <<'EOF'
import socket
s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])
EOF
)
[ -n "$PORT" ] || PORT=28787

export CALENDAR_HOST_PORT=$PORT
export CALENDAR_SERVER_CONTEXT="$REPO/server"
export CALENDAR_VERSION=verify

D="$TMP/deploy"
mkdir -p "$D/secrets"
cp "$REPO"/deploy/compose.yaml "$REPO"/deploy/compose.host-tailscale.yaml "$REPO"/deploy/serve.json "$D/"
(umask 077
 head -c32 /dev/urandom | base64 > "$D/secrets/master_key"
 printf '{"client_id":"dummy.apps.googleusercontent.com","client_secret":"dummy-secret-for-verify"}\n' > "$D/secrets/google_client.json"
 printf 'tskey-dummy-not-real\n' > "$D/secrets/ts_key"
 sed 's#^CALENDAR_PUBLIC_URL=.*#CALENDAR_PUBLIC_URL=https://verify.example.ts.net#' "$REPO/deploy/calendard.env.example" > "$D/calendard.env")
MASTER=$(tr -d '\n' < "$D/secrets/master_key")

c() { docker compose -p "$PROJECT" -f "$D/compose.host-tailscale.yaml" "$@"; }

cleanup() {
	if [ "$KEEP" = 1 ]; then
		echo
		echo "Left running on http://127.0.0.1:$PORT (--keep). Clean up with:"
		echo "  CALENDAR_HOST_PORT=$PORT CALENDAR_SERVER_CONTEXT=$REPO/server docker compose -p $PROJECT -f $D/compose.host-tailscale.yaml down -v; docker rmi $IMG1 $IMG2; rm -rf $TMP"
		return
	fi
	c down -v --remove-orphans >/dev/null 2>&1
	docker rmi "$IMG1" "$IMG2" >/dev/null 2>&1
	rm -rf "$TMP"
}
trap cleanup EXIT

wait_healthy() { # up to 60 s
	local i st
	for i in $(seq 1 30); do
		st=$(docker inspect -f '{{.State.Health.Status}}' "$(c ps -q calendard)" 2>/dev/null)
		[ "$st" = healthy ] && return 0
		sleep 2
	done
	echo "status: $st"; c logs --tail 20 calendard; return 1
}
http() { curl -sS -o /dev/null -w '%{http_code}' "$@"; }

echo "Verifying in $TMP (project $PROJECT, port $PORT)"
echo "1. Image"
check "docker build server/Dockerfile ($IMG1)" docker build -q --build-arg VERSION=verify -t "$IMG1" "$REPO/server"
echo "        size: $(docker image inspect -f '{{.Size}}' "$IMG1" 2>/dev/null | awk '{printf "%.1f MB", $1/1000000}'), arch: $(docker image inspect -f '{{.Architecture}}' "$IMG1" 2>/dev/null)"
user=$(docker image inspect -f '{{.Config.User}}' "$IMG1" 2>/dev/null)
case "$user" in ""|root|0|0:0) bad "image runs as root ($user)";; *) ok "image runs as non-root ($user)";; esac
check "calendard health fails cleanly when nothing listens" bash -c "! docker run --rm -e CALENDAR_ADDR=127.0.0.1:1 $IMG1 health"

echo "2. Compose (host-Tailscale variant)"
check "secrets-init + calendard start" c up -d --build
check "calendard becomes healthy" wait_healthy
[ "$(docker inspect -f '{{.State.ExitCode}}' "$(c ps -aq secrets-init)")" = 0 ] && ok "secrets-init exited 0 (uid 65532 read-only secrets volume)" || bad "secrets-init failed"
DOC=$(c exec -T calendard calendard doctor 2>&1)
printf '%s' "$DOC" | grep -q '^\[fail' && { bad "doctor shows FAIL"; printf '%s\n' "$DOC"; } || ok "doctor shows no FAIL"
check "GET /health 200 on 127.0.0.1:$PORT" bash -c "[ \"\$(curl -fsS http://127.0.0.1:$PORT/health -o /dev/null -w '%{http_code}')\" = 200 ]"
check "published on loopback only" bash -c "docker port \"$(c ps -q calendard)\" 8787/tcp | grep -q '^127.0.0.1:$PORT\$'"
TOKOUT=$(c exec -T calendard calendard token create --name verify --kind device --scope admin 2>&1)
SECRET=$(printf '%s' "$TOKOUT" | sed -n 's/^secret: *//p')
[ -n "$SECRET" ] && ok "token create" || bad "token create: $TOKOUT"
[ "$(http "http://127.0.0.1:$PORT/v1/hello")" = 401 ] && ok "/v1/hello without token is 401" || bad "/v1/hello without token is not 401"
[ "$(http -H "Authorization: Bearer $SECRET" "http://127.0.0.1:$PORT/v1/hello")" = 200 ] && ok "/v1/hello with token is 200" || bad "/v1/hello with token is not 200"
check "calendard backup (encrypted, verified)" c exec -T calendard calendard backup
cid=$(c ps -q calendard)
[ "$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' "$cid")" = true ] && ok "root filesystem read-only" || bad "root filesystem not read-only"
[ "$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "$cid")" = unless-stopped ] && ok "restart policy unless-stopped" || bad "restart policy not unless-stopped"
LOGS=$(c logs calendard secrets-init 2>&1)
printf '%s' "$LOGS" | grep -Eqi 'read-only file system|permission denied|"level":"ERROR"' && bad "errors in logs (read-only/permission/ERROR)" || ok "no read-only/permission/ERROR lines in logs"
printf '%s' "$LOGS" | grep -qF -e "$SECRET" -e "dummy-secret-for-verify" -e "$MASTER" && bad "a secret appears in the logs" || ok "no secrets in logs"

echo "3. Persistence across down/up"
c down >/dev/null 2>&1
check "up again" c up -d
check "healthy again" wait_healthy
[ "$(http -H "Authorization: Bearer $SECRET" "http://127.0.0.1:$PORT/v1/hello")" = 200 ] && ok "same token still works (volume persisted)" || bad "token lost after down/up"
c exec -T calendard calendard doctor 2>&1 | grep 'last backup' | grep -q 'never' && bad "backup not seen by doctor after down/up" || ok "backup still listed after down/up"

echo "4. Upgrade and rollback ($IMG1 -> $IMG2 -> $IMG1)"
CALENDAR_VERSION=verify2 c up -d --build >/dev/null 2>&1
wait_healthy >/dev/null 2>&1
v=$(curl -s -H "Authorization: Bearer $SECRET" "http://127.0.0.1:$PORT/v1/hello")
printf '%s' "$v" | grep -q '"version":"verify2"' && ok "upgrade: hub reports verify2" || bad "upgrade: $v"
CALENDAR_VERSION=verify c up -d >/dev/null 2>&1
wait_healthy >/dev/null 2>&1
v=$(curl -s -H "Authorization: Bearer $SECRET" "http://127.0.0.1:$PORT/v1/hello")
printf '%s' "$v" | grep -q '"version":"verify"' && ok "rollback: hub reports verify, data intact" || bad "rollback: $v"

echo "5. Sidecar variant (config only; no Tailscale auth key needed)"
check "docker compose -f compose.yaml config" docker compose -p "$PROJECT-sidecar" -f "$D/compose.yaml" config -q

echo
if [ "$fail" -eq 0 ]; then echo "PASS ($pass checks)"; else echo "FAIL ($fail failed, $pass passed)"; fi
[ "$fail" -eq 0 ]
