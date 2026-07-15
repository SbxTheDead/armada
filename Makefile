# Armada — build, test, and cross-compile helpers.
# Requires Go 1.26+. No external tooling.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BINDIR  := bin

# Space-separated GOOS/GOARCH pairs for agent cross-compilation. Extend freely;
# Go's toolchain covers the full architecture matrix in the project spec.
AGENT_TARGETS := \
	linux/amd64 linux/arm64 linux/arm linux/386 \
	linux/riscv64 linux/ppc64le linux/s390x linux/mips64le linux/mipsle linux/loong64 \
	darwin/amd64 darwin/arm64 \
	windows/amd64 windows/arm64 windows/386 \
	freebsd/amd64 openbsd/amd64 netbsd/amd64

.PHONY: all build server cli agent test vet cover clean agents

all: build

build: server cli agent

server:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/armada-server ./cmd/armada-server

cli:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/armada ./cmd/armada

agent:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/armada-agent ./cmd/armada-agent

test:
	go test ./...

vet:
	go vet ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

# Cross-compile the agent for every target in AGENT_TARGETS.
agents:
	@mkdir -p $(BINDIR)/agents
	@for t in $(AGENT_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		echo "building agent $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			go build -trimpath -ldflags "$(LDFLAGS)" \
			-o $(BINDIR)/agents/armada-agent-$$os-$$arch$$ext ./cmd/armada-agent || exit 1; \
	done
	@echo "cross-compiled agents in $(BINDIR)/agents/"

clean:
	rm -rf $(BINDIR) coverage.out
