ARG GO_VERSION=1.26.6
ARG GO_RISCV64_SHA256=7b3b526099181b40f5122c8ebf5c851486c7c92977e1f7f39dba9456c6ce42ff
ARG ZIG_VERSION=0.14.0
ARG RUST_VERSION=1.94.0

FROM --platform=$BUILDPLATFORM alpine:3.23 AS go-runtime

ARG GO_VERSION
ARG GO_RISCV64_SHA256

ADD https://go.dev/dl/go${GO_VERSION}.linux-riscv64.tar.gz /tmp/go-linux-riscv64.tar.gz
RUN echo "${GO_RISCV64_SHA256}  /tmp/go-linux-riscv64.tar.gz" | sha256sum -c - \
	&& mkdir -p /opt \
	&& tar -C /opt -xzf /tmp/go-linux-riscv64.tar.gz

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS modules

WORKDIR /workspace
ENV GOMODCACHE=/opt/go-mod-cache

COPY go.mod go.sum ./
RUN go mod download

FROM --platform=$TARGETPLATFORM debian:trixie-slim

ARG TARGETOS
ARG TARGETARCH
ARG ZIG_VERSION
ARG RUST_VERSION

ENV DEBIAN_FRONTEND=noninteractive
ENV CARGO_HOME=/opt/reflektor-cargo
ENV RUSTUP_HOME=/opt/reflektor-rustup
ENV PATH=/opt/reflektor-cargo/bin:/usr/local/go/bin:$PATH

RUN test "${TARGETOS}/${TARGETARCH}" = "linux/riscv64"
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
# The official Go riscv64 toolchain requests the generic Linux RISC-V ELF
# interpreter name. Debian exposes the same LP64D loader under its ABI-specific
# name, so preserve the generic compatibility path when it is absent.
RUN test -e /lib/ld-linux-riscv64-lp64d.so.1 \
	&& if [ ! -e /lib/ld.so.1 ]; then ln -s /lib/ld-linux-riscv64-lp64d.so.1 /lib/ld.so.1; fi

RUN curl -fsSL "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-riscv64-${ZIG_VERSION}.tar.xz" -o /tmp/zig.tar.xz \
	&& tar -xJf /tmp/zig.tar.xz -C /opt \
	&& ln -sf "/opt/zig-linux-riscv64-${ZIG_VERSION}/zig" /usr/local/bin/zig \
	&& zig version

RUN curl --proto '=https' --tlsv1.2 -fsSL https://sh.rustup.rs -o /tmp/rustup-init.sh \
	&& sh /tmp/rustup-init.sh -y --profile minimal \
		--default-host riscv64gc-unknown-linux-gnu \
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

RUN go version | grep -F 'linux/riscv64'

CMD ["/bin/bash", "/workspace/testdata/docker/run-linux-riscv64-bof-tests.sh"]
