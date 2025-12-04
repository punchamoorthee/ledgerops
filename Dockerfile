# Build Stage
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build the new API entry point
RUN CGO_ENABLED=0 GOOS=linux go build -o ledgerops ./cmd/api

# Runtime Stage
FROM alpine:latest
WORKDIR /root/
RUN apk --no-cache add ca-certificates curl
COPY --from=builder /app/ledgerops .
COPY --from=builder /app/db/migrations ./db/migrations
# Copy the benchmark script for container-to-container testing if needed
COPY --from=builder /app/scripts ./scripts 

EXPOSE 8080
CMD ["./ledgerops"]