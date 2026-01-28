# Build stage
FROM golang:1.25.6-alpine AS builder

WORKDIR /app

# Copy source code (includes vendor directory)
COPY . .

# Build the application
# CGO_ENABLED=0 is required for scratch image
# -ldflags="-w -s" reduces binary size
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -ldflags="-w -s" -o besedka .

# Final stage
FROM scratch

# Copy SSL certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /app/besedka /besedka

# Expose the API port
EXPOSE 8080

# Run the binary
ENTRYPOINT ["/besedka"]
