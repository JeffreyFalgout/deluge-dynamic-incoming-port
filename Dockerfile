# syntax=docker/dockerfile:1

## Buildstage ##
FROM golang:1.21 as buildstage

COPY root/* /root-layer/

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN \
  mkdir -p /root-layer/usr/bin && \
  go build -o /root-layer/usr/bin/

## Single layer deployed image ##
FROM scratch

LABEL maintainer="JeffreyFalgout"

# Add files from buildstage
COPY --from=buildstage /root-layer/ /
