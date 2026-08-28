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

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd cmd
COPY internal internal
COPY pkg pkg

COPY --from=ui-builder /app/internal/registry/api/ui/dist /app/internal/registry/api/ui/dist

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-server in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
ARG TARGETARCH
ARG TARGETPLATFORM
ARG LDFLAGS
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags "$LDFLAGS" -o bin/arctl-server cmd/server/main.go

FROM ubuntu:22.04 AS runtime

COPY --from=builder /app/bin/arctl-server /app/bin/arctl-server

LABEL org.opencontainers.image.source=https://github.com/agentregistry-dev/agentregistry
LABEL org.opencontainers.image.description="Agent Registry Server"
LABEL org.opencontainers.image.authors="Agent Registry Creators 🤖"

# Skip the default Content-Type:application/json POST check to keep pre-1.4.1 MCP behavior.
# As of 1.4.1 CORS protection has been changing in the mcp sdk a few times, so this is our safest bet
# Ref: https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.6.1
ENV MCPGODEBUG=disablecontenttypecheck=1

CMD ["/app/bin/arctl-server"]
