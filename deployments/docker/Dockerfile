# Frontend build stage
FROM node:20-alpine AS frontend
WORKDIR /frontend
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY web/dashboard/package.json web/dashboard/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/dashboard/ .
RUN pnpm build

# Go build stage
FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /frontend/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /trader ./cmd/trader

# Runtime stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /trader .
ENTRYPOINT ["/app/trader"]
