FROM golang AS builder
WORKDIR /go/src/gravitybeam
ADD . ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go install

FROM scratch
COPY --from=builder /go/bin/gravitybeam /gravitybeam
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/gravitybeam"]
