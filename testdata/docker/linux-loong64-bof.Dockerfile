ARG GO_VERSION=1.26.6
ARG GO_LOONG64_SHA256=dc7143e4da3e993956e09e19ae72b3330ccea9ceb1cc1fb8e27ea225ac8f8477
ARG ZIG_VERSION=0.14.0
ARG RUST_VERSION=1.94.0

FROM --platform=$BUILDPLATFORM alpine:3.23 AS go-runtime

ARG GO_VERSION
ARG GO_LOONG64_SHA256

ADD https://go.dev/dl/go${GO_VERSION}.linux-loong64.tar.gz /tmp/go-linux-loong64.tar.gz
RUN echo "${GO_LOONG64_SHA256}  /tmp/go-linux-loong64.tar.gz" | sha256sum -c - \
	&& mkdir -p /opt \
	&& tar -C /opt -xzf /tmp/go-linux-loong64.tar.gz

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS modules

WORKDIR /workspace
ENV GOMODCACHE=/opt/go-mod-cache

COPY go.mod go.sum ./
RUN go mod download

FROM ghcr.io/loong64/debian:trixie-slim

ARG TARGETOS
ARG TARGETARCH
ARG ZIG_VERSION
ARG RUST_VERSION

ENV DEBIAN_FRONTEND=noninteractive
ENV CARGO_HOME=/opt/reflektor-cargo
ENV RUSTUP_HOME=/opt/reflektor-rustup
ENV PATH=/opt/reflektor-cargo/bin:/usr/local/go/bin:$PATH

RUN test "${TARGETOS}/${TARGETARCH}" = "linux/loong64"
RUN apt-get update && apt-get install -y --no-install-recommends \
	bash \
	build-essential \
	ca-certificates \
	curl \
	xz-utils \
	file \
	binutils \
	libcurl4-openssl-dev \
	&& rm -rf /var/lib/apt/lists/*
RUN test -e /lib64/ld-linux-loongarch-lp64d.so.1

RUN curl -fsSL "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-loongarch64-${ZIG_VERSION}.tar.xz" -o /tmp/zig.tar.xz \
	&& tar -xJf /tmp/zig.tar.xz -C /opt \
	&& ln -sf "/opt/zig-linux-loongarch64-${ZIG_VERSION}/zig" /usr/local/bin/zig \
	&& zig version

RUN curl --proto '=https' --tlsv1.2 -fsSL https://sh.rustup.rs -o /tmp/rustup-init.sh \
	&& sh /tmp/rustup-init.sh -y --profile minimal \
		--default-host loongarch64-unknown-linux-gnu \
		--default-toolchain "${RUST_VERSION}" \
	&& rm -f /tmp/rustup-init.sh \
	&& rustc --version \
	&& cargo --version

COPY --from=go-runtime /opt/go /usr/local/go
COPY --from=modules /opt/go-mod-cache /opt/go-mod-cache

WORKDIR /workspace

ENV CGO_ENABLED=0
ENV GOCACHE=/tmp/go-build-cache
ENV GOMODCACHE=/opt/go-mod-cache
ENV GOPROXY=off
ENV GOTOOLCHAIN=local
ENV REFLEKTOR_BOF_FIXTURE_DIR=/workspace/test/bof-fixtures
ENV REFLEKTOR_BOF_CORPUS_DIR=/workspace/test/corpus/Situational-Awareness-BOFs

COPY . /workspace

RUN go version | grep -F 'linux/loong64'

CMD ["/bin/bash", "/workspace/testdata/docker/run-linux-loong64-bof-tests.sh"]
