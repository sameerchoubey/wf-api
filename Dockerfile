# Build from the backend/ directory so go.mod and ./cmd/server are present.
# fly deploy should use this folder as context (e.g. `cd backend && fly deploy`).
# syntax=docker/dockerfile:1

ARG GO_VERSION=1.22
FROM golang:${GO_VERSION}-bookworm AS builder
WORKDIR /usr/src/app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /run-app ./cmd/server

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /run-app /run-app
ENV HTTP_ADDR=:8080
EXPOSE 8080
USER nobody
ENTRYPOINT ["/run-app"]
