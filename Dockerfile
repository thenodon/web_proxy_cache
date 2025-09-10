# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.version=${VERSION}" -o web_proxy_cache .

# Final image
FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/web_proxy_cache .
EXPOSE 8080
ENTRYPOINT ["./web_proxy_cache"]