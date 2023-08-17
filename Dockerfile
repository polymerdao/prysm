# based on tools/cross-toolchain/Dockerfile
FROM --platform=$BUILDPLATFORM debian:bullseye-slim as build-env

ARG TARGETARCH
ARG BUILDARCH
ARG BINARY
ARG BAZEL_VERSION

# install gnu/gcc cross-build toolchain (gcc 8.3)
RUN apt-get update && \
    apt-get install -y \
        curl xz-utils patch python \
        gcc g++ git \
        gcc-aarch64-linux-gnu g++-aarch64-linux-gnu

# install llvm/clang cross-build toolchains
ENV INSTALL_LLVM_VERSION=12.0.0
ADD tools/cross-toolchain/install_clang_cross.sh /tmp/install_clang_cross.sh
RUN /tmp/install_clang_cross.sh

WORKDIR /src
COPY ./ .

RUN case "$BUILDARCH" in \
  amd64) arch=x86_64 ;; \
  arm64) arch=arm64  ;; \
  esac && \
  curl -sL "https://releases.bazel.build/${BAZEL_VERSION}/release/bazel-${BAZEL_VERSION}-linux-${arch}" -o /tmp/bazel && \
  chmod +x /tmp/bazel && \
  /tmp/bazel build --config="linux_${TARGETARCH}" "//cmd/$BINARY:$BINARY"

FROM alpine:3.18

ARG BINARY

RUN apk add --no-cache libstdc++ libc6-compat gcompat

COPY --from=build-env "/src/bazel-bin/cmd/$BINARY/${BINARY}_/$BINARY" /usr/bin
