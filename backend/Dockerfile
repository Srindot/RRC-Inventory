# --- Development Stage ---
FROM golang:1.24-alpine AS development
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Create uploads directory
RUN mkdir -p uploads

EXPOSE 8080

# Run the application directly without hot reload
CMD ["go", "run", "main.go"]

# --- Production Builder Stage ---
FROM golang:1.24-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy the Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the Go application into a static binary
# CGO_ENABLED=0 is important for creating a static binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /main .

# --- Production Stage ---
FROM alpine:latest AS production

# Set the working directory
WORKDIR /app

# Copy ONLY the compiled binary from the builder stage
COPY --from=builder /main ./main

# Expose the port that our Go application listens on
EXPOSE 8080

# This is the command that will run when the container starts
CMD ["./main"]
