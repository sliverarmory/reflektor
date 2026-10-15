# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.6
FROM --platform=$TARGETPLATFORM golang:${GO_VERSION}-alpine3.24

# Build each fixture with Alpine's native musl toolchain. GNU-targeted Zig
# fixtures do not establish that Reflektor can load musl-linked images.
RUN apk add --no-cache binutils build-base rust

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .

CMD ["/bin/sh", "/workspace/testdata/docker/run-linux-musl-tests.sh"]
