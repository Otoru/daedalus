GO ?= go
GO_BIN := $(if $(shell $(GO) env GOBIN),$(shell $(GO) env GOBIN),$(shell $(GO) env GOPATH)/bin)

# Resolução de ferramenta. Um override explícito (make BUF=/caminho/buf)
# sempre vence. Caso contrário a ferramenta é procurada primeiro em
# GOBIN/GOPATH-bin (onde `go install` coloca ferramentas com versão
# fixada, como na máquina local) e depois na PATH (onde as actions de setup
# do CI as instalam). Um caminho fixo em GOBIN derrubou o CI do Geppetto:
# lá a action instala na PATH e o binário não existia em GOBIN. Um alvo
# nunca pode passar porque a ferramenta estava ausente: falhas de resolução
# falham alto com instrução de instalação.
find-tool = $(firstword $(wildcard $(GO_BIN)/$(1)) $(shell command -v $(1) 2>/dev/null))

BUF ?= $(call find-tool,buf)
GOLANGCI_LINT ?= $(call find-tool,golangci-lint)

# O CI roda golangci-lint v2.14.0. Binários mais antigos podem entrar em
# pânico com a versão de Go deste módulo ou reportar nada em silêncio, por
# isso o lint exige no mínimo a versão do CI e falha alto em vez de
# produzir um falso verde.
GOLANGCI_LINT_MIN := 2.14.0

# Carimbo de versão para builds fora de um checkout git. O release passa
# VERSION=vX.Y.Z na linha de comando, que sobrescreve este valor.
DEV_VERSION := dev
VERSION := $(shell git describe --always --dirty 2>/dev/null || echo $(DEV_VERSION))

# Matriz de cross-compile como pares GOOS/GOARCH; a entrada windows ganha
# .exe. Espelha os sistemas suportados da seção 13 da spec.
PLATFORMS := linux/amd64 darwin/arm64 windows/amd64

# O comando do subprocesso (spec seção 2) é implementado pela frente do
# núcleo. Enquanto cmd/daedalus não existe, build/build-all degradam para
# uma verificação de compilação de todos os pacotes (inclusive cruzada),
# com aviso alto, em vez de falhar — e voltam a empacotar o binário assim
# que o comando existir. Nenhum arquivo .go é criado por este Makefile.
CMD_DIR := ./cmd/daedalus
HAS_CMD := $(wildcard cmd/daedalus)

.PHONY: generate lint test golden-update bench build build-all

generate:
	@test -n "$(BUF)" || { echo "buf não encontrado; instale com: $(GO) install github.com/bufbuild/buf/cmd/buf@latest (ou passe BUF=/caminho/para/buf)" >&2; exit 1; }
	$(BUF) generate

lint:
	@test -n "$(BUF)" || { echo "buf não encontrado; instale com: $(GO) install github.com/bufbuild/buf/cmd/buf@latest (ou passe BUF=/caminho/para/buf)" >&2; exit 1; }
	@test -n "$(GOLANGCI_LINT)" || { echo "golangci-lint não encontrado; instale com: $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_MIN) (ou passe GOLANGCI_LINT=/caminho/para/golangci-lint)" >&2; exit 1; }
	@version=$$($(GOLANGCI_LINT) version 2>/dev/null | sed -n 's/.*has version \([0-9][0-9.]*\).*/\1/p'); \
	if [ -z "$$version" ]; then \
		echo "não foi possível ler a versão de $(GOLANGCI_LINT); é preciso golangci-lint >= $(GOLANGCI_LINT_MIN)" >&2; exit 1; \
	fi; \
	if ! printf '%s %s\n' "$(GOLANGCI_LINT_MIN)" "$$version" | awk '{ split($$1, m, "."); split($$2, v, "."); for (i = 1; i <= 3; i++) { if (v[i]+0 > m[i]+0) exit 0; if (v[i]+0 < m[i]+0) exit 1 } }'; then \
		echo "golangci-lint $$version em $(GOLANGCI_LINT) é mais antigo que $(GOLANGCI_LINT_MIN), a versão do CI; um linter velho pode perder achados que reprovam este módulo. Instale com: $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_MIN)" >&2; exit 1; \
	fi
	$(BUF) lint
	$(GOLANGCI_LINT) run ./...

test:
	$(GO) test ./...

# Regrava os Layouts normativos somente por ação explícita. Revise o diff
# legível em testdata/golden antes de aceitar uma mudança de compatibilidade.
golden-update:
	$(GO) test ./ -run '^TestLayoutsGoldenCongelados$$' -update -count=1

# Benchmarks de geração vivem no pacote raiz (spec seção 13); -benchmem
# registra as alocações das três cargas normativas.
bench:
	$(GO) test -run '^$$' -bench '^BenchmarkGenerate$$' -benchmem .

build:
ifeq ($(HAS_CMD),)
	@echo "AVISO: cmd/daedalus ainda não existe (frente do núcleo); verificando compilação de todos os pacotes." >&2
	$(GO) build ./...
else
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-X main.Version=$(VERSION)" -o bin/daedalus $(CMD_DIR)
endif

build-all:
ifeq ($(HAS_CMD),)
	@echo "AVISO: cmd/daedalus ainda não existe (frente do núcleo); verificando compilação cruzada de todos os pacotes." >&2
	$(foreach platform,$(PLATFORMS),GOOS=$(word 1,$(subst /, ,$(platform))) GOARCH=$(word 2,$(subst /, ,$(platform))) $(GO) build ./...;)
else
	mkdir -p bin
	$(foreach platform,$(PLATFORMS),GOOS=$(word 1,$(subst /, ,$(platform))) GOARCH=$(word 2,$(subst /, ,$(platform))) $(GO) build -trimpath -ldflags "-X main.Version=$(VERSION)" -o bin/daedalus-$(subst /,-,$(platform))$(if $(filter windows/%,$(platform)),.exe) $(CMD_DIR);)
endif
