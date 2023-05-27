FROM gcr.io/bazel-public/bazel:5.3.0 as build-env

ARG BINARY

WORKDIR prysm
COPY ./ .
RUN bazel build //cmd/$BINARY:$BINARY

FROM alpine:3.18.0

ARG BINARY

COPY --from=build-env /home/ubuntu/prysm/bazel-bin/cmd/$BINARY/${BINARY}_/$BINARY /usr/bin

ENV BINARY=$BINARY
ENTRYPOINT ["$BINARY"]