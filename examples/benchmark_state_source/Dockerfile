# Use the official Golang image as the base image
FROM golang:1.23-alpine AS builder

# deps
WORKDIR /app
COPY go.mod .
RUN go mod tidy

# code
COPY . .
RUN go build -o main ./examples/tree_state_source

# Start a new stage from scratch
FROM alpine:latest

# Set the Current Working Directory inside the container
WORKDIR /root/

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /app/main .

# Expose ports
EXPOSE 19700
EXPOSE 18700

# Command to run the executable
CMD ["./main"]
