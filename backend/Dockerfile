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

# Timezone data, so TZ (e.g. Asia/Kolkata) actually resolves - without this
# Go silently falls back to UTC and due dates shift by hours.
# su-exec lets the entrypoint drop from root to the app user.
RUN apk add --no-cache tzdata su-exec \
    && addgroup -S app \
    && adduser -S -G app -H -h /app app

# Set the working directory
WORKDIR /app

# Copy ONLY the compiled binary from the builder stage
COPY --from=builder /main ./main
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# Photos are written here; the entrypoint also fixes ownership on the volume.
RUN mkdir -p /app/uploads && chown app:app /app/uploads \
    && chmod 755 /usr/local/bin/docker-entrypoint.sh

# Expose the port that our Go application listens on
EXPOSE 8080

# The entrypoint starts as root, chowns /app/uploads, then runs the app as
# "app". There is deliberately no USER line: an existing uploads volume holds
# root-owned photos, and the app could not write next to them without that
# one-time fix.
ENTRYPOINT ["docker-entrypoint.sh"]

# This is the command that will run when the container starts
CMD ["./main"]
