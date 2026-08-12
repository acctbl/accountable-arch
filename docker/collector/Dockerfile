# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc AS build

ARG TARGETARCH
ARG TARGETOS

RUN go install go.opentelemetry.io/collector/cmd/builder@v0.158.0

WORKDIR /src

COPY go.mod go.sum ./
COPY gen/go ./gen/go
COPY internal ./internal
COPY docker/collector/builder-config.yaml ./docker/collector/builder-config.yaml

RUN builder --skip-compilation --config docker/collector/builder-config.yaml

WORKDIR /src/dist/collector

RUN CGO_ENABLED=0 GOARCH="$TARGETARCH" GOOS="$TARGETOS" \
    go build -trimpath -ldflags="-s -w -buildid=" -o /out/accountable-collector . && \
    mkdir -p /out/runtime

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/accountable-collector /usr/local/bin/accountable-collector
COPY --from=build --chown=65532:65532 --chmod=0700 /out/runtime /run/accountable

VOLUME ["/run/accountable"]
USER 65532:65532
EXPOSE 4318 4319
ENTRYPOINT ["/usr/local/bin/accountable-collector"]
