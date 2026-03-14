FROM golang:1.26-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY . .
RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux go build -o vsphere-eventlogger main.go

FROM scratch

WORKDIR /app
COPY --from=builder /app/vsphere-eventlogger .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENTRYPOINT ["/app/vsphere-eventlogger"]
