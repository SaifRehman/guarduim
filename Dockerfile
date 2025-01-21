# Use Go base image
FROM golang:1.23 as builder

# Set the Current Working Directory inside the container
WORKDIR /app


# Copy the source code into the container
COPY . .

# Build the Go app without running go mod tidy
RUN GOOS=linux go build -o guarduim-controller .

# Start a new stage from scratch
FROM debian:bullseye-slim

# Install necessary packages
RUN apt-get update && apt-get install -y ca-certificates

# Set the Current Working Directory inside the container
WORKDIR /root/

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /app/guarduim-controller .

# Expose the port the app runs on (if necessary)
EXPOSE 8080

# Command to run the executable
CMD ["./guarduim-controller"]