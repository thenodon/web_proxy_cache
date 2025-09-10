# Build stage
FROM golang:1.23-alpine AS builder
# Set necessary environmet variables needed for our image
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
ARG VERSION=dev
RUN go build -ldflags "-X main.version=${VERSION}" -o web_proxy_cache .

# Final image
FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/web_proxy_cache .
EXPOSE 8080
ENTRYPOINT ["./web_proxy_cache"]