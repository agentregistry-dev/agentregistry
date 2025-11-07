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
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

# alpine install make
RUN apk add --no-cache make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd cmd
COPY internal internal

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


# Use TargetARCH to download the correct Docker binary
RUN wget https://download.docker.com/linux/static/stable/x86_64/docker-28.5.1.tgz
RUN tar -xvf docker-28.5.1.tgz
RUN mv docker/docker /usr/local/bin/docker
RUN rm -rf docker-28.5.1.tgz docker

RUN DOCKER_CONFIG=${DOCKER_CONFIG:-$HOME/.docker}
RUN mkdir -p $DOCKER_CONFIG/cli-plugins
RUN curl -SL https://github.com/docker/compose/releases/download/v2.40.3/docker-compose-linux-x86_64 -o $DOCKER_CONFIG/cli-plugins/docker-compose
RUN chmod +x $DOCKER_CONFIG/cli-plugins/docker-compose

COPY --from=builder /app/bin/arctl-server /app/bin/arctl-server

COPY .env.example .env

CMD ["/app/bin/arctl-server"]