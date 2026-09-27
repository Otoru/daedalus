GO ?= go
GO_BIN := $(if $(shell $(GO) env GOBIN),$(shell $(GO) env GOBIN),$(shell $(GO) env GOPATH)/bin)

# Resolução de ferramenta. Um override explícito (make BUF=/caminho/buf)
# sempre vence. Caso contrário a ferramenta é procurada primeiro em
# GOBIN/GOPATH-bin (onde `go install` coloca ferramentas com versão
# fixada, como na máquina local) e depois na PATH (onde as actions de setup
# do CI as instalam). Um alvo nunca pode passar porque a ferramenta estava
# ausente: falhas de resolução falham alto com instrução de instalação.
find-tool = $(firstword $(wildcard $(GO_BIN)/$(1)) $(shell command -v $(1) 2>/dev/null))

BUF ?= $(call find-tool,buf)

.PHONY: generate build test

generate:
	@test -n "$(BUF)" || { echo "buf não encontrado; instale com: $(GO) install github.com/bufbuild/buf/cmd/buf@latest (ou passe BUF=/caminho/para/buf)" >&2; exit 1; }
	$(BUF) generate

build:
	$(GO) build ./...

test:
	$(GO) test ./...
