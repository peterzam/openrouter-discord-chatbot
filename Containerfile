ARG GOLANG_VERSION="1.23.4"

FROM docker.io/library/golang:$GOLANG_VERSION-alpine as builder
WORKDIR /go/src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-s' -o ./app

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/src/app /app
ENTRYPOINT ["/app"]
