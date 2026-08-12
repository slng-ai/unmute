#!/usr/bin/env bash
# Manage Daily phone numbers for the Pipecat Daily route.
#
# This is operator tooling for someone else's account, not part of the compiler.
# It exists because "buy a number on your Daily domain" is four REST calls that
# are easy to get wrong in ways that cost money, and because one of them cannot
# be undone for two weeks.
#
# Nothing here is required to compile or deploy. `unmute` never calls it.
#
# Every call goes to Daily's REST API and authenticates with DAILY_API_KEY, which
# is a key from your Daily developer account. That is a different key from the
# Pipecat Cloud public key (pk_...) that starts agent sessions. Verified against
# docs.daily.co/reference/rest-api/phone-numbers and
# docs.pipecat.ai/pipecat/telephony/daily-phone-numbers, 2026-08-12.
set -euo pipefail

API="https://api.daily.co/v1"

usage() {
	cat <<'EOF'
Manage Daily phone numbers for the Pipecat Daily route.

Usage:
  scripts/daily-phone-number.sh available [<region>]
  scripts/daily-phone-number.sh buy [<number>] [--yes]
  scripts/daily-phone-number.sh list
  scripts/daily-phone-number.sh attach <number> <org-id> <agent-name>
  scripts/daily-phone-number.sh release <id> [--yes]

Commands:
  available   Search numbers you could buy. Region is a two-letter code, default CA.
  buy         Buy a number. Costs money. With no number, Daily picks one for you.
  list        Show the numbers you already own, with the id each one is known by.
  attach      Point a number at a deployed Pipecat Cloud agent, so calls reach it.
  release     Give a number up. Impossible for 14 days after purchase, then permanent.

Environment:
  DAILY_API_KEY   Required. From your Daily developer account. Never printed.

Notes:
  A number's id, not the number itself, is what dial-out wants as its callerId.
  Dial-out is a paid Daily feature you have to ask for, and international
  dial-out is granted separately per domain. A cold transfer dials its
  destination, so it needs dial-out too.
EOF
}

die() {
	printf 'error: %s\n' "$1" >&2
	exit 1
}

need_key() {
	[[ -n "${DAILY_API_KEY:-}" ]] || die "DAILY_API_KEY is not set. Export a key from your Daily developer account."
}

# call METHOD PATH [BODY] — the key rides the header and is never echoed.
call() {
	local method="$1" path="$2" body="${3:-}"
	local -a args=(--silent --show-error --fail-with-body
		--request "$method" --url "$API/$path"
		--header "Authorization: Bearer $DAILY_API_KEY")
	if [[ -n "$body" ]]; then
		args+=(--header 'Content-Type: application/json' --data "$body")
	fi
	curl "${args[@]}"
}

# confirm PROMPT — refuses unless the operator types yes, or passed --yes.
confirm() {
	if [[ "${ASSUME_YES:-}" == "1" ]]; then
		return 0
	fi
	if [[ ! -t 0 ]]; then
		die "this spends money or is irreversible; re-run with --yes to confirm non-interactively"
	fi
	printf '%s [yes/no] ' "$1"
	local reply
	read -r reply
	[[ "$reply" == "yes" ]] || die "cancelled"
}

# Pull --yes out of the arguments wherever it appears.
ARGS=()
ASSUME_YES=0
for arg in "$@"; do
	case "$arg" in
	--yes | -y) ASSUME_YES=1 ;;
	-h | --help | help) usage && exit 0 ;;
	*) ARGS+=("$arg") ;;
	esac
done
set -- ${ARGS[@]+"${ARGS[@]}"}

[[ $# -ge 1 ]] || {
	usage
	exit 1
}
command=$1
shift

case "$command" in
available)
	need_key
	region="${1:-CA}"
	printf 'Numbers available in %s:\n' "$region" >&2
	call GET "list-available-numbers?region=$region"
	;;

buy)
	need_key
	number="${1:-}"
	if [[ -n "$number" ]]; then
		[[ "$number" == +* ]] || die "number must be E.164, starting with +, got $number"
		confirm "Buy $number on the Daily account behind DAILY_API_KEY? This costs money, and Daily does not allow releasing a number for 14 days."
		body="{\"number\": \"$number\"}"
	else
		confirm "Buy a number Daily picks for you? This costs money, you do not get to choose the number, and Daily does not allow releasing it for 14 days."
		body='{}'
	fi
	call POST "buy-phone-number" "$body"
	printf '\nKeep the id above. Dial-out wants the id as its callerId, not the number.\n' >&2
	printf 'Next: attach it to a deployed agent.\n' >&2
	printf '  scripts/daily-phone-number.sh attach <number> <org-id> <agent-name>\n' >&2
	;;

list)
	need_key
	call GET "purchased-phone-numbers"
	;;

attach)
	need_key
	[[ $# -eq 3 ]] || die "attach needs <number> <org-id> <agent-name>"
	number=$1
	org=$2
	agent=$3
	[[ "$number" == +* ]] || die "number must be E.164, starting with +, got $number"
	webhook="https://api.pipecat.daily.co/v1/public/webhooks/$org/$agent/dialin"
	printf 'Pointing %s at %s\n' "$number" "$webhook" >&2
	# Daily replaces the whole pinless_dialin list, so attaching a second number
	# means sending both. Say so rather than let someone silently unhook one.
	printf 'Note: this replaces the domain pinless_dialin list. If you have other\n' >&2
	printf 'numbers attached, use the dashboard instead, or send them all together.\n' >&2
	confirm "Continue?"
	call POST "" "{\"properties\": {\"pinless_dialin\": [{\"phone_number\": \"$number\", \"room_creation_api\": \"$webhook\"}]}}"
	printf '\nAttach after the agent reports ready. A number pointed at an agent that\n' >&2
	printf 'is not deployed gives the caller silence, not an error you can see.\n' >&2
	;;

release)
	need_key
	[[ $# -eq 1 ]] || die "release needs the number's <id>, which \`list\` prints"
	id=$1
	confirm "Release the number with id $id? This is permanent, and it fails if the number is less than 14 days old."
	call DELETE "release-phone-number/$id"
	;;

*)
	usage
	die "unknown command: $command"
	;;
esac
