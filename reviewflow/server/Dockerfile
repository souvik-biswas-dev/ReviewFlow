# syntax=docker/dockerfile:1.7
#
# Production image for the ReviewFlow Node backend.
#   Stage 1 (builder) : install everything, compile TypeScript to dist/.
#   Stage 2 (deps)    : install production dependencies only.
#   Stage 3 (runtime) : copy dist/ + prod node_modules, run as a non-root user.

# --- builder ---------------------------------------------------------------
FROM node:24-alpine AS builder
WORKDIR /src

# Cache the dependency install in its own layer so app changes don't bust it.
COPY package.json package-lock.json ./
RUN npm ci

COPY tsconfig.json ./
COPY scripts ./scripts
COPY src ./src
RUN npm run build

# --- production dependencies ----------------------------------------------
FROM node:24-alpine AS deps
WORKDIR /src

COPY package.json package-lock.json ./
RUN npm ci --omit=dev

# --- runtime --------------------------------------------------------------
FROM node:24-alpine AS runtime
ENV NODE_ENV=production
WORKDIR /app

COPY --from=deps  /src/node_modules ./node_modules
COPY --from=builder /src/dist ./dist
COPY package.json ./

# `node` (uid 1000) ships with the base image; never run the server as root.
USER node
EXPOSE 8080

# Exec form, so node is PID 1 and receives SIGTERM directly — that's what the
# graceful shutdown handler in src/index.ts hangs off.
CMD ["node", "dist/index.js"]
