# Use official Golang base image
FROM golang:1.23 AS builder

# Set the working directory in the container
WORKDIR /app

# Copy the Go modules manifests
COPY go.mod go.sum ./

# Download dependencies
RUN go mod tidy

# Copy the rest of the application source code
COPY . .

# Build the Go application
RUN GOOS=linux  go build -o guarduim-controller .

# Start a new stage to minimize the final image size
FROM alpine:latest

# Install necessary dependencies for running Go binary
RUN apk --no-cache add ca-certificates

# Copy the Go binary from the builder stage
COPY --from=builder /app/guarduim-controller /usr/local/bin/guarduim-controller

# Set the entrypoint to the Go binary
ENTRYPOINT ["/usr/local/bin/guarduim-controller"]

# Expose port (this is a placeholder, adjust if you need to expose a port)
EXPOSE 8080

# Default command to run the binary
CMD ["guarduim-controller"]
