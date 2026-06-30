ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui-builder
# alpine install make
RUN apk add --no-cache make

WORKDIR /app

COPY Makefile ./
COPY ui/package.json ui/package-lock.json ./
COPY ui ui
RUN mkdir -p internal/registry/api/ui/dist
RUN make build-ui

ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

# alpine install make
RUN apk add --no-cache make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd cmd
COPY internal internal
COPY pkg pkg

COPY --from=ui-builder /app/internal/registry/api/ui/dist /app/internal/registry/api/ui/dist

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
ARG TARGETARCH
ARG TARGETPLATFORM
ARG LDFLAGS
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags "$LDFLAGS" -o bin/arctl-server cmd/server/main.go

FROM ubuntu:22.04 AS runtime

RUN apt-get update && apt-get install -y \
    curl \
    wget \
    unzip \
    && rm -rf /var/lib/apt/lists/*


# Install Docker CLI and Compose plugin for the target architecture
ARG TARGETARCH
RUN DOCKER_ARCH=$(case "${TARGETARCH:-}" in \
        (amd64) echo "x86_64" ;; \
        (arm64) echo "aarch64" ;; \
        (*) echo "x86_64" ;; \
    esac) && \
    wget https://download.docker.com/linux/static/stable/${DOCKER_ARCH}/docker-29.6.0.tgz && \
    tar -xvf docker-29.6.0.tgz && \
    mv docker/docker /usr/local/bin/docker && \
    rm -rf docker-29.6.0.tgz docker

# Install Docker Compose plugin
ARG TARGETARCH
RUN set -eux; \
    COMPOSE_ARCH=$(case "${TARGETARCH:-}" in \
        (amd64) echo "x86_64" ;; \
        (arm64) echo "aarch64" ;; \
        (*) echo "x86_64" ;; \
    esac); \
    COMPOSE_NAME=docker-compose-linux-${COMPOSE_ARCH}; \
    COMPOSE_URL=https://github.com/docker/compose/releases/download/v5.2.0; \
    COMPOSE_DIR=/tmp/docker-compose-download; \
    for attempt in 1 2 3 4 5; do \
        rm -rf ${COMPOSE_DIR}; \
        mkdir -p ${COMPOSE_DIR}; \
        if curl -fL --retry 3 --retry-delay 2 --retry-all-errors ${COMPOSE_URL}/${COMPOSE_NAME} -o ${COMPOSE_DIR}/${COMPOSE_NAME} && \
            curl -fL --retry 3 --retry-delay 2 --retry-all-errors ${COMPOSE_URL}/${COMPOSE_NAME}.sha256 -o ${COMPOSE_DIR}/${COMPOSE_NAME}.sha256 && \
            (cd ${COMPOSE_DIR} && sha256sum -c ${COMPOSE_NAME}.sha256); then \
            break; \
        fi; \
        if [ "$attempt" = "5" ]; then \
            exit 1; \
        fi; \
        sleep 2; \
    done; \
    install -d /usr/local/lib/docker/cli-plugins; \
    install -m 0755 ${COMPOSE_DIR}/${COMPOSE_NAME} /usr/local/lib/docker/cli-plugins/docker-compose; \
    rm -rf ${COMPOSE_DIR}; \
    docker compose version

COPY --from=builder /app/bin/arctl-server /app/bin/arctl-server

LABEL org.opencontainers.image.source=https://github.com/agentregistry-dev/agentregistry
LABEL org.opencontainers.image.description="Agent Registry Server"
LABEL org.opencontainers.image.authors="Agent Registry Creators 🤖"

# Skip the default Content-Type:application/json POST check to keep pre-1.4.1 MCP behavior.
# As of 1.4.1 CORS protection has been changing in the mcp sdk a few times, so this is our safest bet
# Ref: https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.6.1
ENV MCPGODEBUG=disablecontenttypecheck=1

CMD ["/app/bin/arctl-server"]
