# Multi-stage build for the Armada control-plane server. Produces a small,
# static, non-root image suitable for running on a VPS.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache modules first (no external deps today, but keeps builds fast if added).
COPY go.mod ./
RUN go mod download

COPY . .
# CGO off => static binaries that run in a minimal image.
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/armada-server ./cmd/armada-server \
 && go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/armada ./cmd/armada

# Cross-compile the management agent for every served target so the running
# control plane can hand devices the right binary via /manage.
ARG AGENT_TARGETS="linux/amd64 linux/arm64 linux/arm linux/386 linux/riscv64 linux/ppc64le linux/s390x linux/mips64le linux/mipsle linux/loong64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 windows/386 freebsd/amd64 openbsd/amd64 netbsd/amd64"
RUN mkdir -p /out/agents && for t in ${AGENT_TARGETS}; do \
      os="${t%/*}"; arch="${t#*/}"; ext=""; [ "$os" = "windows" ] && ext=".exe"; \
      echo "building agent $os/$arch"; \
      GOOS="$os" GOARCH="$arch" go build -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o "/out/agents/armada-agent-$os-$arch$ext" ./cmd/armada-agent || exit 1; \
    done

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
 && adduser -D -u 10001 armada
COPY --from=build /out/armada-server /usr/local/bin/armada-server
COPY --from=build /out/armada /usr/local/bin/armada
COPY --from=build /out/agents /app/agents
ENV ARMADA_AGENT_DIST_DIR=/app/agents
USER armada
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/armada-server"]
