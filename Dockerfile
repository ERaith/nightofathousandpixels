# syntax=docker/dockerfile:1

# Night of a Thousand Pixels - production image.
#
# Three stages produce two images:
#
#   final     the shipped app. Distroless, non-root, one static binary and the
#             CA bundle. No shell, no package manager, and none of the build
#             tooling - templ, sqlc, goose and air never reach it. Generated
#             code (sqlc output, *_templ.go) is committed, so the build only
#             ever needs `go build`.
#
#   migrator  a separate image holding the goose binary and the migration SQL.
#             It exists so that migrations can run to completion, and be seen
#             to fail, before the app is allowed to start - see the `migrate`
#             service in docker-compose.yml. Keeping it out of `final` is the
#             point: the shipped image cannot run migrations, so nothing in
#             production can quietly re-run or repair them.
#
# Go 1.26 is a hard floor. sqlc v1.31.1 and golangci-lint v2.13.2 both require
# it, so go.mod says `go 1.26.0` and a 1.25.x builder fails outright.
ARG GO_IMAGE=golang:1.27-alpine
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot

# ---------------------------------------------------------------------------
# builder - compiles the server and the goose CLI from the pinned module
# ---------------------------------------------------------------------------
FROM ${GO_IMAGE} AS builder

WORKDIR /src

# Dependencies first so that editing source does not re-download the module
# cache on every build.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

# CGO off keeps the binary static, which is what distroless/static expects.
# -trimpath drops local paths from the binary; -s -w drop the symbol table.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags='-s -w' \
        -o /out/server \
        ./cmd/server

# goose is built from the version pinned in go.mod's tool block, so the
# migrator runs exactly the goose that `make migrate-up` runs locally.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags='-s -w' \
        -o /out/goose \
        github.com/pressly/goose/v3/cmd/goose

# ---------------------------------------------------------------------------
# migrator - goose plus the migration SQL, and nothing else
# ---------------------------------------------------------------------------
FROM ${RUNTIME_IMAGE} AS migrator

COPY --from=builder /out/goose /usr/local/bin/goose
COPY internal/store/migrations /migrations

ENV GOOSE_DRIVER=postgres \
    GOOSE_MIGRATION_DIR=/migrations

# No shell and no wrapper script on purpose. goose is PID 1, so its exit code
# is the container's exit code and a failed migration cannot be caught,
# retried or "resolved" by anything in between. The 2025 app deployed with
# `prisma migrate deploy || (prisma db push --accept-data-loss && echo
# Database initialized)`, which turned a failed migration into a success
# message and an empty database. There is deliberately nowhere to put that
# here.
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/goose"]
CMD ["up"]

# ---------------------------------------------------------------------------
# final - the shipped image
# ---------------------------------------------------------------------------
FROM ${RUNTIME_IMAGE} AS final

# Needed to verify TLS certificates when calling the OIDC provider.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/server /server

# The container always listens on 8080; the host-side port is the compose
# publish mapping's business, so it can vary per agent slot.
ENV PORT=8080
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/server"]

# ---------------------------------------------------------------------------
# mockoidcd - the OIDC provider the development and end-to-end stacks sign in
# against. NEVER part of the shipped image.
# ---------------------------------------------------------------------------
#
# Development-only, and structurally so rather than by convention. This stage
# sits BELOW `final` and nothing above it refers to it, so `docker build
# --target final` never builds it and the shipped image cannot contain it. It
# derives from `builder` only to reuse that stage's module download and build
# cache.
#
# The stronger guarantee is in the Go build graph rather than here:
# e2e/mockoidcd is its own main package, so `go list -deps ./cmd/server` does
# not mention oauth2-proxy/mockoidc at all. That is the check worth running --
# a Dockerfile stage is a convention, but an absent import is a fact. See the
# `no-mock-provider-in-server` target in the Makefile.
#
# The base is alpine rather than distroless because this image is allowed to
# have a shell: the compose healthcheck uses busybox wget, and there is no
# reason to harden an image that exists to be thrown away.
FROM builder AS mockoidc-builder

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -o /out/mockoidcd \
        ./e2e/mockoidcd

FROM alpine:3.22 AS mockoidcd

RUN adduser -D -H -u 65532 nonroot

COPY --from=mockoidc-builder /out/mockoidcd /usr/local/bin/mockoidcd

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/mockoidcd"]
