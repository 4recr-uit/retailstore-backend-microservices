# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://proxy.golang.org,direct
ENV GOTOOLCHAIN=auto

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o orders .

# Runtime stage
FROM alpine:3.20

RUN apk --no-cache add ca-certificates curl

WORKDIR /app

COPY --from=builder /app/orders /app/orders

EXPOSE 8080

USER 1000:1000

ENTRYPOINT ["/app/orders"]
