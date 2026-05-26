# syntax=docker/dockerfile:1.7
#
# Production image for the ReviewFlow frontend.
#   Stage 1 (builder) : npm ci + vite build -> static SPA in /app/build
#   Stage 2 (caddy)   : Caddy serves the SPA AND reverse-proxies API/WS calls
#                       to the Go backend. Single edge container, automatic
#                       HTTPS via Let's Encrypt.

# --- builder ---------------------------------------------------------------
FROM node:22-alpine AS builder
WORKDIR /app

# Cache dependencies independently of source changes.
COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

# --- caddy edge -----------------------------------------------------------
FROM caddy:2-alpine

# The Caddyfile templates the domain / backend host via env so the same image
# works for staging and production.
COPY Caddyfile /etc/caddy/Caddyfile
COPY --from=builder /app/build /srv

EXPOSE 80 443
