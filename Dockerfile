# syntax=docker/dockerfile:1

## Buildstage ##
FROM golang:1.21 as buildstage

WORKDIR /src
COPY . .
RUN \
  mkdir /root-layer/ && \
  cp -r /src/root/* /root-layer/ && \
  mkdir -p /root-layer/usr/bin && \
  go build . && \
  cp deluge-dynamic-incoming-port /root-layer/usr/bin

## Single layer deployed image ##
FROM scratch

LABEL maintainer="JeffreyFalgout"

# Add files from buildstage
COPY --from=buildstage /root-layer/ /
