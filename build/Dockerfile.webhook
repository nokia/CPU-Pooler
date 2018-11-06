# Build stage
FROM golang:alpine AS build-env

ARG PLUGIN_PATH=github.com/nokia/CPU-Pooler

RUN apk update && apk add curl git

RUN mkdir -p ${GOPATH}/src/${PLUGIN_PATH}

RUN curl -fsSL -o /usr/local/bin/dep https://github.com/golang/dep/releases/download/v0.5.0/dep-linux-amd64 && chmod +x /usr/local/bin/dep

WORKDIR ${GOPATH}/src/${PLUGIN_PATH}

ADD Gopkg.* ./

RUN dep ensure --vendor-only

ADD . ./

RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o cpu-device-webhook ${PLUGIN_PATH}/cmd/webhook


# Final image creation

FROM alpine:latest

ARG PLUGIN_PATH=github.com/nokia/CPU-Pooler
COPY --from=build-env /go/src/${PLUGIN_PATH}/cpu-device-webhook .

ENTRYPOINT ["/cpu-device-webhook"]
