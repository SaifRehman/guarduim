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

# Install necessary packages including `oc` client and `file` utility
RUN apt-get update && apt-get install -y ca-certificates curl file \
    && curl -Lo /tmp/oc.tar.gz hhttps://mirror.openshift.com/pub/openshift-v4/clients/oc/latest/linux/oc.tar.gz \
    && ls -l /tmp/oc.tar.gz \
    && file /tmp/oc.tar.gz \
    && tar -xvzf /tmp/oc.tar.gz -C /usr/local/bin \
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
