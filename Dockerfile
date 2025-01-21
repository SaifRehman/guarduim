# Use an image with the required glibc version
FROM go:1.21 AS builder

# Set the working directory
WORKDIR /app

# Copy the Go module files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod tidy

# Copy the source code
COPY . .

# Build the Go binary
RUN GOOS=linux GOARCH=amd64 go build -o guarduim-controller .

# Use a compatible image for the runtime (e.g., debian:bullseye)
FROM go:1.21.0-bullseye

# Copy the built binary from the builder stage
COPY --from=builder /app/guarduim-controller /usr/local/bin/guarduim-controller

# Set the entrypoint for the container
ENTRYPOINT ["/usr/local/bin/guarduim-controller"]

# Specify the port your application will listen on (if applicable)
EXPOSE 8080
