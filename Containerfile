# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:3.18

WORKDIR /app

# Add non root user
RUN snapshotter -D -g '' snapshotter

# Copy binary from builder
COPY --from=builder /app/main .

# Use non root user
USER snapshotter

# Command to run
ENTRYPOINT ["/app/main"]