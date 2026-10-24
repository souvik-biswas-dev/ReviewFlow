# syntax=docker/dockerfile:1.7
#
# Production image for the ReviewFlow Go backend.
#   Stage 1: compile a static binary.
#   Stage 2: distroless, non-root, nothing but the binary.

FROM golang:1.23-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-mod=mod

# Dependencies first so source edits don't bust the download layer.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
# Exec form: the server is PID 1 and receives SIGTERM for graceful shutdown.
ENTRYPOINT ["/server"]
