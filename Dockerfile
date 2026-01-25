# Build webui
FROM node:23-alpine AS webui
WORKDIR /app/webui
RUN npm install -g pnpm
COPY webui/package.json webui/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY webui/ ./
RUN pnpm exec vite build

# Build Go binary
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webui /app/internal/server/webui ./internal/server/webui
RUN CGO_ENABLED=1 go build -o xagent ./cmd/xagent

# Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/xagent .
EXPOSE 6464
CMD ["./xagent", "server"]
