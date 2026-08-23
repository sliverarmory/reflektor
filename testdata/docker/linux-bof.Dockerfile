ARG GO_VERSION=1.26.6
ARG ZIG_VERSION=0.14.0
ARG RUST_VERSION=1.94.0

FROM --platform=$TARGETPLATFORM golang:${GO_VERSION}-bookworm

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG ZIG_VERSION
ARG RUST_VERSION

ENV DEBIAN_FRONTEND=noninteractive
ENV CARGO_HOME=/opt/reflektor-cargo
ENV RUSTUP_HOME=/opt/reflektor-rustup
ENV PATH=/opt/reflektor-cargo/bin:${PATH}

RUN test "${TARGETOS}/${TARGETARCH}/${TARGETVARIANT}" = "linux/arm/v7"
RUN apt-get update && apt-get install -y --no-install-recommends \
	build-essential \
	ca-certificates \
	curl \
	xz-utils \
	file \
	binutils \
	libcurl4-openssl-dev \
	&& rm -rf /var/lib/apt/lists/*
# CGO-free PureGo binaries still carry Go's generic ARM EABI interpreter path.
# Debian armhf names the same loader explicitly, so preserve the generic path
# when the base image does not already provide its compatibility symlink.
RUN test -e /lib/ld-linux-armhf.so.3 \
	&& if [ ! -e /lib/ld-linux.so.3 ]; then ln -s /lib/ld-linux-armhf.so.3 /lib/ld-linux.so.3; fi

RUN curl -fsSL "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-armv7a-${ZIG_VERSION}.tar.xz" -o /tmp/zig.tar.xz \
	&& tar -xJf /tmp/zig.tar.xz -C /opt \
	&& ln -sf "/opt/zig-linux-armv7a-${ZIG_VERSION}/zig" /usr/local/bin/zig \
	&& zig version

RUN curl --proto '=https' --tlsv1.2 -fsSL https://sh.rustup.rs -o /tmp/rustup-init.sh \
	&& sh /tmp/rustup-init.sh -y --profile minimal \
		--default-host armv7-unknown-linux-gnueabihf \
		--default-toolchain "${RUST_VERSION}" \
	&& rm -f /tmp/rustup-init.sh \
	&& rustc --version \
	&& cargo --version

WORKDIR /workspace

ENV CGO_ENABLED=0
ENV GOARM=7
ENV GOCACHE=/tmp/go-build-cache
ENV GOMODCACHE=/opt/go-mod-cache
ENV GOPROXY=off
ENV GOTOOLCHAIN=local
ENV REFLEKTOR_BOF_FIXTURE_DIR=/workspace/test/bof-fixtures
ENV REFLEKTOR_BOF_CORPUS_DIR=/workspace/test/corpus/Situational-Awareness-BOFs

COPY go.mod go.sum ./
RUN GOPROXY=https://proxy.golang.org go mod download
COPY . /workspace

CMD ["/bin/bash", "/workspace/testdata/docker/run-linux-bof-tests.sh"]
