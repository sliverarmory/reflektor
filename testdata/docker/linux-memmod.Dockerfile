# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.6
ARG ZIG_VERSION=0.14.0
ARG RUST_VERSION=1.94.0

FROM --platform=$TARGETPLATFORM golang:${GO_VERSION}-bookworm

ARG TARGETARCH
ARG ZIG_VERSION
ARG RUST_VERSION

ENV DEBIAN_FRONTEND=noninteractive
ENV CARGO_HOME=/opt/reflektor-cargo
ENV RUSTUP_HOME=/opt/reflektor-rustup
ENV PATH=/opt/reflektor-cargo/bin:${PATH}

RUN apt-get update && apt-get install -y --no-install-recommends \
	ca-certificates \
	curl \
	xz-utils \
	file \
	binutils \
	libcurl4-openssl-dev \
	bash \
	&& rm -rf /var/lib/apt/lists/*

RUN set -eux; \
	case "${TARGETARCH}" in \
		amd64) zig_arch="x86_64" ;; \
		arm64) zig_arch="aarch64" ;; \
		386) zig_arch="x86" ;; \
		*) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
	esac; \
	curl -fsSL "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-${zig_arch}-${ZIG_VERSION}.tar.xz" -o /tmp/zig.tar.xz; \
	tar -xJf /tmp/zig.tar.xz -C /opt; \
	ln -sf "/opt/zig-linux-${zig_arch}-${ZIG_VERSION}/zig" /usr/local/bin/zig; \
	zig version

RUN set -eux; \
	case "${TARGETARCH}" in \
		amd64) rust_host="x86_64-unknown-linux-gnu" ;; \
		arm64) rust_host="aarch64-unknown-linux-gnu" ;; \
		386) rust_host="i686-unknown-linux-gnu" ;; \
		*) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
	esac; \
	curl --proto '=https' --tlsv1.2 -fsSL https://sh.rustup.rs -o /tmp/rustup-init.sh; \
	sh /tmp/rustup-init.sh -y --profile minimal --default-host "${rust_host}" --default-toolchain "${RUST_VERSION}"; \
	rm -f /tmp/rustup-init.sh; \
	rustc --version; \
	cargo --version

WORKDIR /workspace
COPY . /workspace

CMD ["/bin/bash", "/workspace/testdata/docker/run-linux-memmod-tests.sh"]
