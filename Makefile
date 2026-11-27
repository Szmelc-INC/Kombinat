# Kombinat MkII core: build, test, lint and check (AGENTS.md Commands).
GO ?= go
BIN := bin/kombinat
export CGO_ENABLED := 0

.PHONY: build test smoke lint check completions clean

build:
	$(GO) build -trimpath -o $(BIN) ./core/cmd/kombinat

test:
	$(GO) test ./...

smoke: build
	bash tests/smoke.sh $(BIN)

lint:
	@out=$$(gofmt -l core); if [ -n "$$out" ]; then echo "gofmt: not formatted:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	shellcheck tools/*.sh tests/*.sh
	bash tools/lint.sh

check: lint build test smoke

completions: build
	@mkdir -p completions
	$(BIN) completion bash > completions/kombinat.bash
	$(BIN) completion zsh > completions/_kombinat
	$(BIN) completion fish > completions/kombinat.fish

clean:
	rm -rf bin
