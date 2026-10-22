#!/usr/bin/env bash
# Pre-flight for the first start of the hub on the Mac mini. Safe to re-run: it never overwrites files and never prints secrets.
#   deploy/first-run.sh               host-Tailscale variant (default, compose.host-tailscale.yaml)
#   deploy/first-run.sh --sidecar     Tailscale sidecar variant (compose.yaml; also needs secrets/ts_key)
#   deploy/first-run.sh --make-key    also create secrets/master_key if it does not exist (never replaces one)
# Walkthrough: docs/first-run.md
set -u

VARIANT=host
MAKE_KEY=0
for a in "$@"; do
	case "$a" in
		--sidecar) VARIANT=sidecar ;;
		--make-key) MAKE_KEY=1 ;;
		-h|--help) sed -n '2,6p' "$0"; exit 0 ;;
		*) echo "unknown option: $a" >&2; exit 2 ;;
	esac
done

cd "$(dirname "$0")/.." || exit 1
ROOT=$(pwd)
SEC=deploy/secrets
ENVF=deploy/calendard.env
fails=0
ok()   { printf '  ok    %s\n' "$*"; }
warn() { printf '  WARN  %s\n' "$*"; }
bad()  { printf '  FAIL  %s\n' "$*"; fails=$((fails + 1)); }

mode_of() { stat -f %Lp "$1" 2>/dev/null || stat -c %a "$1" 2>/dev/null; }

if [ "$VARIANT" = host ]; then
	COMPOSE="docker compose -f deploy/compose.host-tailscale.yaml"
else
	COMPOSE="docker compose -f deploy/compose.yaml"
fi

echo "1. Prerequisites"
command -v docker >/dev/null 2>&1 && ok "docker" || bad "docker not found (install Docker Desktop or OrbStack)"
if command -v docker >/dev/null 2>&1; then
	docker compose version >/dev/null 2>&1 && ok "docker compose v2" || bad "docker compose v2 plugin missing"
	docker info >/dev/null 2>&1 && ok "docker daemon reachable" || bad "docker daemon not running (start Docker Desktop / OrbStack)"
fi
command -v jq >/dev/null 2>&1 && ok "jq" || bad "jq not found (brew install jq)"
TS=""
if command -v tailscale >/dev/null 2>&1; then TS=tailscale
elif [ -x /Applications/Tailscale.app/Contents/MacOS/Tailscale ]; then TS=/Applications/Tailscale.app/Contents/MacOS/Tailscale; fi
if [ -n "$TS" ]; then ok "tailscale CLI ($TS)"; else
	if [ "$VARIANT" = host ]; then bad "tailscale CLI not found (see docs/first-run.md, step 4)"; else warn "tailscale CLI not found on the host (only needed to read your tailnet name)"; fi
fi

DNSNAME=""
if [ -n "$TS" ] && command -v jq >/dev/null 2>&1; then
	if json=$("$TS" status --json 2>/dev/null); then
		DNSNAME=$(printf '%s' "$json" | jq -r '.Self.DNSName // empty' | sed 's/\.$//')
		state=$(printf '%s' "$json" | jq -r '.BackendState // empty')
		[ "$state" = Running ] && ok "tailscale is $state" || bad "tailscale state is '$state' (log in / connect first)"
		[ -n "$DNSNAME" ] && ok "this machine: $DNSNAME" || warn "no DNSName in 'tailscale status' (enable MagicDNS)"
		if [ "$VARIANT" = host ] && [ "$(printf '%s' "$json" | jq '(.CertDomains // []) | length')" = 0 ]; then
			warn "HTTPS certificates are not enabled for the tailnet (admin console, DNS, enable HTTPS Certificates)"
		fi
	else
		bad "'tailscale status --json' failed (is Tailscale running and logged in?)"
	fi
fi

echo "2. Secrets in $SEC (mode 0600; contents are never shown)"
mkdir -p "$SEC"
check_perm() {
	m=$(mode_of "$1")
	if [ "$m" = 600 ] || [ "$m" = 400 ]; then ok "$1 mode $m"; else bad "$1 mode is $m, run: chmod 600 $1"; fi
}
KEY="$SEC/master_key"
if [ ! -f "$KEY" ] && [ "$MAKE_KEY" = 1 ]; then
	( umask 077; head -c32 /dev/urandom | base64 > "$KEY" ) && ok "created $KEY (BACK IT UP somewhere off this Mac, e.g. a password manager)"
fi
if [ ! -f "$KEY" ]; then
	bad "$KEY missing. Create it: (umask 077; head -c32 /dev/urandom | base64 > $KEY)   or re-run with --make-key"
else
	n=$( (base64 -d < "$KEY" 2>/dev/null || base64 -D < "$KEY" 2>/dev/null) | wc -c | tr -d ' ')
	if [ "$n" = 32 ]; then ok "$KEY decodes to 32 bytes"; else
		raw=$(tr -d '[:space:]' < "$KEY")
		if printf '%s' "$raw" | grep -Eq '^[0-9a-fA-F]{64}$'; then ok "$KEY is 64 hex chars (32 bytes)"; else bad "$KEY is not a 32-byte key (base64 of 32 bytes, or 64 hex chars)"; fi
	fi
	check_perm "$KEY"
fi
GC="$SEC/google_client.json"
if [ ! -f "$GC" ]; then
	bad "$GC missing. Format: {\"client_id\": \"...apps.googleusercontent.com\", \"client_secret\": \"...\"} (docs/first-run.md, step 1)"
elif command -v jq >/dev/null 2>&1; then
	if jq -e '(.client_id|type=="string" and length>0) and (.client_secret|type=="string" and length>0)' "$GC" >/dev/null 2>&1; then
		ok "$GC is valid JSON with client_id and client_secret"
		jq -e '.client_id | test("\\.apps\\.googleusercontent\\.com$")' "$GC" >/dev/null 2>&1 || warn "client_id does not end in .apps.googleusercontent.com; is it the Web client?"
	else
		bad "$GC must be JSON with non-empty client_id and client_secret (Google's downloaded file nests them under \"web\"; copy the two values out)"
	fi
	check_perm "$GC"
fi
if [ "$VARIANT" = sidecar ]; then
	if [ -s "$SEC/ts_key" ]; then ok "$SEC/ts_key present"; check_perm "$SEC/ts_key"; else bad "$SEC/ts_key missing (Tailscale auth key, tagged tag:calendar)"; fi
fi

echo "3. Settings in $ENVF"
if [ ! -f "$ENVF" ]; then
	cp deploy/calendard.env.example "$ENVF" && chmod 600 "$ENVF" && ok "created $ENVF from the example"
	if [ -n "$DNSNAME" ] && [ "$VARIANT" = host ]; then
		# the file is brand new, so editing the one placeholder line is safe
		tmp=$(mktemp) && sed "s#^CALENDAR_PUBLIC_URL=.*#CALENDAR_PUBLIC_URL=https://$DNSNAME#" "$ENVF" > "$tmp" && cat "$tmp" > "$ENVF"; rm -f "$tmp"
		ok "set CALENDAR_PUBLIC_URL=https://$DNSNAME"
	else
		warn "edit $ENVF and set CALENDAR_PUBLIC_URL to https://<this-mac>.<tailnet>.ts.net"
	fi
else
	ok "$ENVF exists (left untouched)"
	cur=$(sed -n 's/^CALENDAR_PUBLIC_URL=//p' "$ENVF" | tail -n 1)
	if [ -z "$cur" ]; then bad "CALENDAR_PUBLIC_URL is not set in $ENVF"
	elif printf '%s' "$cur" | grep -q 'example-tailnet'; then bad "CALENDAR_PUBLIC_URL is still the example value ($cur)"
	elif [ "$VARIANT" = host ] && [ -n "$DNSNAME" ] && [ "$cur" != "https://$DNSNAME" ]; then warn "CALENDAR_PUBLIC_URL is $cur but this Mac is https://$DNSNAME (fine only if you serve under another name)"
	else ok "CALENDAR_PUBLIC_URL=$cur"; fi
fi
PUB=$(sed -n 's/^CALENDAR_PUBLIC_URL=//p' "$ENVF" 2>/dev/null | tail -n 1)
[ -n "$PUB" ] || PUB="https://<mac-mini>.<tailnet>.ts.net"
m=$(mode_of "$ENVF"); [ "$m" = 600 ] || warn "$ENVF mode is $m (chmod 600 $ENVF is tidy; it holds no secrets by default)"

if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:8787 -sTCP:LISTEN >/dev/null 2>&1 && [ "$VARIANT" = host ]; then
	warn "something already listens on port 8787 (the hub itself if it is already running; otherwise see docs/first-run.md, common failures)"
fi

echo
if [ "$fails" -gt 0 ]; then
	echo "$fails problem(s) above. Fix them and run this script again (it is safe to repeat)."
	exit 1
fi

echo "All checks passed. Next, from $ROOT:"
echo
echo "  1. Google redirect URI to register (exactly):  $PUB/auth/google/callback"
echo "  2. $COMPOSE up -d --build"
echo "  3. $COMPOSE exec calendard calendard doctor        # nothing may say FAIL"
if [ "$VARIANT" = host ]; then
	echo "  4. ${TS:-tailscale} serve --bg --https=443 http://127.0.0.1:8787"
	echo "     ${TS:-tailscale} serve status"
else
	echo "  4. (the sidecar publishes HTTPS by itself; check: $COMPOSE logs tailscale)"
fi
echo "  5. curl -fsS $PUB/health                           # from the Mac and from another tailnet device"
echo "  6. $COMPOSE exec calendard calendard token create --name iphone --kind device --scope admin"
echo "     (prints the cal_... secret once; type it into the app)"
echo "  7. In the app, pairing address:  $PUB"
echo "  8. Back up secrets/master_key somewhere that is not this Mac."
exit 0
