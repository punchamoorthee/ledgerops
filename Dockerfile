# STAGE 1: Build the Go binary
# Use a 'builder' image to keep our final image small
FROM golang:1.25.2 AS builder

# Set up the container's environment
WORKDIR /app

# Copy the Go module files
COPY go.mod go.sum ./

# Download all the dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the Go app. 
# CGO_ENABLED=0 is important for a small, static binary.
RUN CGO_ENABLED=0 GOOS=linux go build -o /ledgerops_server ./cmd/api/

# STAGE 2: Create the final, small image
FROM 1.25.2:latest

WORKDIR /

# Copy *only* the built binary from the 'builder' stage
COPY --from=builder /ledgerops_server .

# This is the command that will run when the container starts
CMD ["/ledgerops_server"]