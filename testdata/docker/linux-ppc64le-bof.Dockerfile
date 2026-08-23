ARG GO_VERSION=1.26.6
ARG GO_PPC64LE_SHA256=232b65543a42eda95df6a63f76235c1795bb535eba5c74e509faec71bc648388
ARG ZIG_VERSION=0.14.0
ARG RUST_VERSION=1.94.0

FROM --platform=$BUILDPLATFORM alpine:3.23 AS go-runtime

ARG GO_VERSION
ARG GO_PPC64LE_SHA256

ADD https://go.dev/dl/go${GO_VERSION}.linux-ppc64le.tar.gz /tmp/go-linux-ppc64le.tar.gz
RUN echo "${GO_PPC64LE_SHA256}  /tmp/go-linux-ppc64le.tar.gz" | sha256sum -c - \
	&& mkdir -p /opt \
	&& tar -C /opt -xzf /tmp/go-linux-ppc64le.tar.gz

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

RUN test "${TARGETOS}/${TARGETARCH}" = "linux/ppc64le"
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
# Debian's native ppc64le image exposes the ELFv2 dynamic loader at the
# architecture-standard path. The pinned Go distribution itself is statically
# linked, but retain this check so native test tooling cannot silently run with
# the wrong PowerPC ABI image.
RUN test -e /lib64/ld64.so.2 \
	&& test -e /usr/lib/powerpc64le-linux-gnu/ld64.so.2

RUN curl -fsSL "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-powerpc64le-${ZIG_VERSION}.tar.xz" -o /tmp/zig.tar.xz \
	&& tar -xJf /tmp/zig.tar.xz -C /opt \
	&& ln -sf "/opt/zig-linux-powerpc64le-${ZIG_VERSION}/zig" /usr/local/bin/zig \
	&& zig version

RUN curl --proto '=https' --tlsv1.2 -fsSL https://sh.rustup.rs -o /tmp/rustup-init.sh \
	&& sh /tmp/rustup-init.sh -y --profile minimal \
		--default-host powerpc64le-unknown-linux-gnu \
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

RUN go version | grep -F 'linux/ppc64le'

CMD ["/bin/bash", "/workspace/testdata/docker/run-linux-ppc64le-bof-tests.sh"]
