#!/bin/sh
# Copy the unmute-core docs into docs-site/core and their tab into docs.json.
#
#   scripts/sync_core_docs.sh [ref]     # ref defaults to main
#
# unmute-core writes its pages at the paths they have on this site
# (docs/core/...), and its docs/docs.json holds one tab. Mintlify builds from
# one repository, so the pages are copied here, not linked. The commit they came
# from is written to docs-site/core/.source-sha. Never edit a page under
# docs-site/core: fix it in unmute-core and run this again.
set -eu

repo=slng-ai/unmute-core
ref="${1:-main}"
site="$(cd "$(dirname "$0")/../docs-site" && pwd)"

# Pages that describe Unmute behaviour main does not have yet. Remove a name
# here in the same change that makes the page true.
held_back="start/unmute-and-core"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

sha="$(gh api "repos/$repo/commits/$ref" --jq .sha)"
gh api "repos/$repo/tarball/$sha" >"$tmp/core.tgz"
tar -xzf "$tmp/core.tgz" -C "$tmp" --strip-components=1

for page in $held_back; do
	rm "$tmp/docs/core/$page.mdx"
done

rm -rf "$site/core"
cp -R "$tmp/docs/core" "$site/core"
echo "$sha" >"$site/core/.source-sha"

held_json="$(printf '%s\n' $held_back | jq -R '"core/" + .' | jq -s .)"
jq --indent 2 --slurpfile core "$tmp/docs/docs.json" --argjson held "$held_json" '
  def drop_held: if type == "object" then del(.icon) | .pages |= map(select(. as $p | $held | index($p) | not) | drop_held) else . end;
  .navigation.tabs |= (map(select(.tab != "Unmute Core")) + [
    $core[0].navigation.tabs[0] | .tab = "Unmute Core" | .groups |= map(drop_held)
  ])
' "$site/docs.json" >"$tmp/docs.json"
mv "$tmp/docs.json" "$site/docs.json"

echo "docs-site/core synced to $repo@$sha"
