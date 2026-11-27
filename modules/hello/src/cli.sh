#!/bin/sh
# Hello CLI entrypoint: prints one greeting to stdout and exits 0.
# Inside the mesh the core sets KOMBINAT_CFG_MODULE_NAME from config.
# Standalone: sh modules/hello/src/cli.sh [name]
set -eu

name=${1:-${KOMBINAT_CFG_MODULE_NAME:-world}}
printf 'Hello, %s!\n' "$name"
