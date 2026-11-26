# Kombinat MkII core: build, test, lint and check (AGENTS.md Commands).
GO ?= go
BIN := bin/kombinat
export CGO_ENABLED := 0
# Module shell sources and module tests (modules/NAME/tests/*.sh).
MODULE_SH := $(wildcard modules/*/src/*.sh modules/*/tests/*.sh)
MODULE_TESTS := $(wildcard modules/*/tests/*.sh)
# Go modules carry their own go.mod, so the root `go test ./...` skips them.
MODULE_GO := $(wildcard modules/*/go.mod)

.PHONY: build test smoke modules-test lint check completions clean

build:
	$(GO) build -trimpath -o $(BIN) ./core/cmd/kombinat

test:
	$(GO) test ./...

smoke: build
	bash tests/smoke.sh $(BIN)

# Every module's own tests: each tests/*.sh runs with KOMBINAT_BIN set to the
# built core, and each Go module runs go vet and go test in its directory.
modules-test: build
	@set -e; for t in $(MODULE_TESTS); do echo "==> $$t"; KOMBINAT_BIN="$(abspath $(BIN))" bash "$$t"; done
	@set -e; for m in $(MODULE_GO); do d=$$(dirname "$$m"); echo "==> go vet and go test in $$d"; (cd "$$d" && $(GO) vet ./... && $(GO) test ./...); done

lint:
	@out=$$(gofmt -l core modules); if [ -n "$$out" ]; then echo "gofmt: not formatted:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	shellcheck tools/*.sh tests/*.sh $(MODULE_SH)
	bash tools/lint.sh

check: lint build test smoke modules-test

completions: build
	@mkdir -p completions
	$(BIN) completion bash > completions/kombinat.bash
	$(BIN) completion zsh > completions/_kombinat
	$(BIN) completion fish > completions/kombinat.fish

clean:
	rm -rf bin
