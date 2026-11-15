# syntax=docker/dockerfile:1

# Build stage: the Go image whose version equals the toolchain line in go.mod.
FROM golang:1.27.4-alpine3.24 AS build

# Fail instead of downloading another toolchain. GOTOOLCHAIN=local alone checks
# only the go line, not toolchain, so the explicit check below catches an image
# and a toolchain line that drifted apart, for example after a Dependabot update
# of this image.
ENV GOTOOLCHAIN=local
# The image has no make; the build goes through `make build`.
RUN apk add --no-cache make

WORKDIR /src
COPY go.mod go.sum ./
RUN grep -qx "toolchain $(go env GOVERSION)" go.mod \
    || { echo "go.mod toolchain does not match the image's $(go env GOVERSION)" >&2; exit 1; }

COPY . .
# Generates the Swagger specification, then builds a static binary; a failed
# generation fails the image build. The cache mounts download only the modules
# the build needs, not the test-only ones a plain `go mod download` would
# fetch, and keep both caches between builds.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make build

# Runtime stage: Alpine, a non-root user and the single binary. Migrations and
# the Swagger specification are embedded in it. BusyBox wget serves the
# compose healthcheck.
FROM alpine:3.24.4

# A fixed numeric UID and GID: Kubernetes' runAsNonRoot cannot verify a user
# given by name, and 65532 is the distroless "nonroot" convention. No home
# directory: the service writes no files. The binary stays owned by root, so
# the process cannot overwrite it.
RUN addgroup -S -g 65532 app && adduser -S -H -u 65532 -G app app
COPY --from=build /src/bin/subscription-service /usr/local/bin/subscription-service
USER 65532:65532
EXPOSE 8080

# Exec form: the binary is PID 1 and receives SIGTERM itself. A shell form
# would wrap it in /bin/sh -c, which does not forward the signal.
ENTRYPOINT ["/usr/local/bin/subscription-service"]
