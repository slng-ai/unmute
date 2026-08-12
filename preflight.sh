#!/usr/bin/env bash
# Check a secrets file before sending it to LiveKit Cloud. Prints names and
# verdicts, never values.
#
#   ./preflight.sh examples/human-transfer/build/livekit
set -uo pipefail

BUILD="${1:-examples/human-transfer/build/livekit}"
ENV_FILE="$BUILD/.env"
EXAMPLE="$BUILD/.env.example"
fail=0

[ -f "$ENV_FILE" ] || { echo "missing $ENV_FILE"; exit 1; }
[ -f "$EXAMPLE" ] || { echo "missing $EXAMPLE"; exit 1; }

echo "== keys that are not valid shell identifiers =="
# LiveKit Cloud writes your secrets into /etc/run/env and sources it, so a name
# starting with a digit becomes `export: not a valid identifier` at boot and that
# secret is simply absent. Rename it or drop it.
bad=$(grep -oE '^[^#=]+' "$ENV_FILE" | tr -d ' ' | grep -vE '^[A-Za-z_][A-Za-z0-9_]*$' || true)
if [ -n "$bad" ]; then
  echo "$bad" | sed 's/^/  INVALID  /'
  fail=1
else
  echo "  none"
fi

echo
echo "== names this build needs =="
# Everything above the "Supplied for you" comment block in .env.example. The
# platform injects the LIVEKIT_* trio itself and the managed SIP service owns
# Redis, so those are not yours to set for a Cloud deploy.
needed=$(sed -n '1,/^# Supplied for you/p' "$EXAMPLE" | grep -oE '^[A-Z_][A-Z0-9_]*(?==)' 2>/dev/null \
  || sed -n '1,/^# Supplied for you/p' "$EXAMPLE" | grep -oE '^[A-Z_][A-Z0-9_]*=' | tr -d '=')
for name in $needed; do
  value=$(grep -E "^${name}=" "$ENV_FILE" | head -1 | cut -d= -f2-)
  if ! grep -qE "^${name}=" "$ENV_FILE"; then
    # No special case for a trunk ID any more: none is asked for. Inbound records
    # are created by the emitted telephony-setup.sh, which resolves the trunk by
    # phone number (SCHEMA N36, 2026-08-12).
    echo "  MISSING  $name"
    fail=1
  elif [ -z "$value" ]; then
    echo "  EMPTY    $name"
    fail=1
  else
    echo "  ok       $name (${#value} chars)"
  fi
done

echo
echo "== names you are sending that this build does not use =="
for name in $(grep -oE '^[A-Za-z_][A-Za-z0-9_]*=' "$ENV_FILE" | tr -d '='); do
  grep -qE "^${name}=" "$EXAMPLE" || echo "  extra    $name"
done

echo
if [ "$fail" -eq 0 ]; then
  echo "PREFLIGHT OK"
else
  echo "PREFLIGHT FAILED: fix the lines above before deploying"
fi
exit "$fail"
