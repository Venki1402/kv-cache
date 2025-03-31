FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod ./
# If you have a go.sum file, uncomment the next line
# COPY go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application with optimization flags
RUN CGO_ENABLED=0 GOOS=linux go build -o kv-cache -ldflags="-s -w" .

# Create a minimal production image
FROM alpine:latest

# Add runtime dependencies if needed
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/kv-cache .

# Expose the required port
EXPOSE 7171

# Run the application
CMD ["./kv-cache"]