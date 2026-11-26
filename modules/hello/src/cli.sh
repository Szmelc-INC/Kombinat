#!/bin/sh
# Hello CLI entrypoint: prints one greeting to stdout and exits 0.
# Standalone: sh modules/hello/src/cli.sh [name]
set -eu

name=${1:-world}
printf 'Hello, %s!\n' "$name"
