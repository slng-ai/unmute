#!/bin/sh
# Compile every example on every target it declares, then run each generated
# project's own pinned ruff over it: `ruff check` with the rules its
# pyproject.toml selects, and `ruff format --check`.
#
# This is the CI gate for the readability rules in internal/generate/pyproject.go.
# `--only-group dev` installs the project's pinned checkers and none of its
# provider SDKs: ruff reads source and imports nothing, and the pin stays in
# the one place pyproject.go writes it. ty does need the SDKs, so it lives in
# `make smoke`.
#
# The examples are copied first: compiling in place would replace a developer's
# own build/ folder, and with it the dev.log `unmute dev` writes there.
set -eu

# compile formats with the ruff on PATH and writes raw output without one, and
# raw output is not laid out the way `ruff format --check` wants. The raw
# output's lint is `make smoke`'s job; this checks what a user with ruff gets.
if ! command -v ruff >/dev/null; then
  echo "lint-emitted: ruff is not on PATH, so compile would not format; install the version internal/generate/pyproject.go pins" >&2
  exit 1
fi

unmute=${UNMUTE:-bin/unmute}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cp -R examples "$work/"
for package in "$work"/examples/*/; do
  [ -f "$package/agent.yaml" ] || continue
  rm -rf "$package/build"
  "$unmute" compile "$package" >/dev/null
done

failed=0
for project in "$work"/examples/*/build/*/; do
  [ -f "$project/pyproject.toml" ] || continue
  name=${project#"$work"/examples/}
  if ! (cd "$project" && uv run -q --only-group dev ruff check . && uv run -q --only-group dev ruff format --check .); then
    echo "lint-emitted: $name does not pass its own ruff gate" >&2
    failed=1
  fi
done
exit "$failed"
