# syntax=docker/dockerfile:1.7
#
# Production image for the ReviewFlow Go backend.
#   Stage 1 (builder)  : compile a static binary.
#   Stage 2 (runtime)  : copy it into distroless/static — no shell, no package
#                        manager, runs as a non-root user.

# --- builder ---------------------------------------------------------------
FROM golang:1.26-alpine AS builder
WORKDIR /src

# Cache module downloads in their own layer so app changes don't bust them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO disabled so the result is a fully static binary that runs unmodified
# in distroless/static. -trimpath + -s -w strips paths + symbols for size.
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- runtime --------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

# CA roots are baked into distroless/static, so HTTPS to GitHub / Gemini
# works out of the box without copying any /etc/ssl files.
COPY --from=builder /out/server /server

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/server"]
