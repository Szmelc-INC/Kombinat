#!/bin/sh
# Hello CLI entrypoint: prints one greeting to stdout and exits 0.
# Inside the mesh the core sets KOMBINAT_CFG_MODULE_NAME from config and
# KOMBINAT_EVENTS to a file for this module's own events (one JSON per line).
# Standalone: sh modules/hello/src/cli.sh [name]
set -eu

name=${1:-${KOMBINAT_CFG_MODULE_NAME:-world}}
printf 'Hello, %s!\n' "$name"

# The event carries no user input, so it needs no JSON escaping.
if [ -n "${KOMBINAT_EVENTS:-}" ]; then
  printf '%s\n' '{"action":"hello.greeted","summary":"greeted one name"}' >>"$KOMBINAT_EVENTS"
fi
