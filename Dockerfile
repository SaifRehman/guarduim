# Use Go base image
FROM golang:1.23 as builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy the Go Modules manifests
COPY go.mod go.sum ./

# Copy the source code into the container
COPY . .

# Build the Go app without running go mod tidy
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o guarduim-controller .

# Start a new stage from scratch
FROM debian:bullseye-slim

# Install necessary packages, including `oc` client
RUN apt-get update && apt-get install -y ca-certificates curl \
    && curl -Lo /tmp/oc.tar.gz https://github.com/openshift/okd/releases/download/release-4.10.16/openshift-client-linux-4.10.16.tar.gz \
    && tar -xvf /tmp/oc.tar.gz -C /usr/local/bin \
    && rm -f /tmp/oc.tar.gz

# Set the Current Working Directory inside the container
WORKDIR /root/

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /app/guarduim-controller .

# Expose the port the app runs on (if necessary)
EXPOSE 8080
RUN chmod +x guarduim-controller

# Command to run the executable
CMD ["./guarduim-controller"]
