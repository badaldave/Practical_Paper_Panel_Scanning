# Stage 1: Build
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -o api-server ./cmd/api/main.go

# Stage 2: Runtime
FROM alpine:3.19

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/api-server .

# Install ca-certificates
RUN apk --no-cache add ca-certificates

EXPOSE 8080

CMD ["./api-server"]
