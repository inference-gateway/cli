FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o mcp-demo-server main.go

# Final stage - minimal runtime image
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata curl

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/mcp-demo-server .

# Expose default port
EXPOSE 3000

# Run the server
CMD ["./mcp-demo-server"]
