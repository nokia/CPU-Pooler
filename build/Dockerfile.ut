FROM golang:1.16.6-alpine3.14 AS builder
MAINTAINER Levente Kale <levente.kale@nokia.com>

RUN apk add --no-cache ca-certificates make git bash sudo

ENV POOLER_TEST_DIR="${GOPATH}/src/github.com/nokia/CPU-Pooler/test"
ENV CGO_ENABLED 0

WORKDIR ${GOPATH}/src/github.com/nokia/CPU-Pooler