# Use Golang as the base image for building
FROM golang:1.21 AS builder

WORKDIR /app

# Copy source files
COPY . .

# Download dependencies
RUN go mod tidy

# Build the controller binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o guarduim-controller

# Use a minimal base image for runtime
FROM registry.access.redhat.com/ubi8/ubi-minimal

WORKDIR /root/

# Copy the built binary from builder stage
COPY --from=builder /app/guarduim-controller .

# Set entrypoint
ENTRYPOINT ["./guarduim-controller"]
