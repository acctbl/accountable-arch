# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM --platform=$BUILDPLATFORM golang:1.27.0-alpine3.23@sha256:3747dcba41c8b0db3211fda4db61638b980e17ac5bb3c94460a975a9cfe19395 AS build

ARG TARGETARCH
ARG TARGETOS

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY gen/go ./gen/go
COPY internal ./internal

RUN CGO_ENABLED=0 GOARCH="$TARGETARCH" GOOS="$TARGETOS" go build -trimpath -ldflags="-s -w -buildid=" -o /out/accountable-api ./cmd/api && \
    CGO_ENABLED=0 GOARCH="$TARGETARCH" GOOS="$TARGETOS" go build -trimpath -ldflags="-s -w -buildid=" -o /out/accountable-bootstrap ./cmd/bootstrap && \
    CGO_ENABLED=0 GOARCH="$TARGETARCH" GOOS="$TARGETOS" go build -trimpath -ldflags="-s -w -buildid=" -o /out/accountable-migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOARCH="$TARGETARCH" GOOS="$TARGETOS" go build -trimpath -ldflags="-s -w -buildid=" -o /out/accountable-preflight ./cmd/preflight && \
    mkdir -p /out/runtime

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

ADD --checksum=sha256:e5bb2084ccf45087bda1c9bffdea0eb15ee67f0b91646106e466714f9de3c7e3 https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem /etc/ssl/certs/aws-rds-global-bundle.pem
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/accountable-api /usr/local/bin/accountable-api
COPY --from=build /out/accountable-bootstrap /usr/local/bin/accountable-bootstrap
COPY --from=build /out/accountable-migrate /usr/local/bin/accountable-migrate
COPY --from=build /out/accountable-preflight /usr/local/bin/accountable-preflight
COPY --from=build --chown=65532:65532 /out/runtime /run/accountable
COPY --chmod=0555 docker/runtime-entrypoint.sh /usr/local/bin/accountable-entrypoint

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/accountable-entrypoint"]
CMD ["api"]
