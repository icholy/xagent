# Build webui
FROM --platform=$BUILDPLATFORM node:23-alpine AS webui
WORKDIR /app/webui
RUN npm install -g pnpm
COPY webui/package.json webui/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY webui/ ./
RUN pnpm exec vite build

# Build Go binaries
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webui /app/internal/server/webui ./internal/server/webui
RUN CGO_ENABLED=0 GOARCH=$TARGETARCH go build -o xagent ./cmd/xagent
RUN CGO_ENABLED=0 GOARCH=amd64 go build -o prebuilt/xagent-linux-amd64 ./cmd/xagent
RUN CGO_ENABLED=0 GOARCH=arm64 go build -o prebuilt/xagent-linux-arm64 ./cmd/xagent

# Server image
FROM alpine:3.21 AS server
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/xagent .
EXPOSE 6464
CMD ["./xagent", "server"]

# Runner image
FROM alpine:3.21 AS runner
RUN apk add --no-cache ca-certificates docker-cli
WORKDIR /app
COPY --from=builder /app/xagent .
COPY --from=builder /app/prebuilt/ /app/prebuilt/
ENV XAGENT_PREBUILT_DIR=/app/prebuilt
CMD ["./xagent", "runner"]
