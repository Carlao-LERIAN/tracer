FROM golang:1.25-alpine3.22 AS builder

# Pin package versions for reproducible builds (Alpine 3.22)
ARG CA_CERTIFICATES_VERSION=20250911-r0
ARG GIT_VERSION=2.49.1-r0
ARG TZDATA_VERSION=2025c-r0

WORKDIR /app

RUN apk add --no-cache \
    ca-certificates=${CA_CERTIFICATES_VERSION} \
    git=${GIT_VERSION} \
    tzdata=${TZDATA_VERSION} \
    && update-ca-certificates

WORKDIR /tracer

# Copy only go.mod and go.sum first to cache dependencies
COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG TARGETARCH
SHELL ["/bin/ash", "-e", "-o", "pipefail", "-c"]
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o /app/tracer ./cmd/app
# Production image using distroless for minimal attack surface (Ring Standards)
# Note: distroless has no shell - use orchestrator health checks (Kubernetes probes)
FROM gcr.io/distroless/static-debian12:nonroot AS prod

WORKDIR /app

COPY --from=builder /app/tracer /app/tracer
COPY --from=builder /tracer/migrations /app/migrations

# distroless:nonroot already runs as non-root user (uid 65532)

EXPOSE 8080 7001

ENTRYPOINT ["/app/tracer"]

