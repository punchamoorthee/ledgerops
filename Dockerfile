# Build Stage
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o benchmark ./cmd/benchmark
RUN CGO_ENABLED=0 GOOS=linux go build -o seeder ./cmd/seeder

# Runtime Stage
FROM alpine:latest
WORKDIR /root/
RUN apk --no-cache add ca-certificates curl
COPY --from=builder /app/api .
COPY --from=builder /app/benchmark .
COPY --from=builder /app/seeder .
COPY --from=builder /app/db/migrations ./db/migrations

EXPOSE 8080
CMD ["./api"]