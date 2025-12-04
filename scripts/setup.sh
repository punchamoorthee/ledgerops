#!/bin/bash
set -e

echo "=== LedgerOps Setup ==="
echo ""

# Check prerequisites
command -v go >/dev/null 2>&1 || { echo "Error: Go is not installed"; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "Error: Docker is not installed"; exit 1; }
command -v docker-compose >/dev/null 2>&1 || { echo "Error: Docker Compose is not installed"; exit 1; }

echo "✓ Prerequisites check passed"
echo ""

# Install Go tools
echo "Installing Go development tools..."
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
echo "✓ Tools installed"
echo ""

# Create .env file if it doesn't exist
if [ ! -f .env ]; then
    echo "Creating .env file from template..."
    cp .env.example .env
    echo "✓ .env file created"
else
    echo "✓ .env file already exists"
fi
echo ""

# Start services
echo "Starting Docker services..."
docker-compose up -d
echo "✓ Services started"
echo ""

# Wait for PostgreSQL
echo "Waiting for PostgreSQL to be ready..."
sleep 5
echo "✓ PostgreSQL ready"
echo ""

# Run migrations
echo "Running database migrations..."
make migrate-up
echo "✓ Migrations complete"
echo ""

# Build application
echo "Building application..."
make build
echo "✓ Build complete"
echo ""

echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "  1. Run 'make run' to start the server"
echo "  2. Visit http://localhost:8080"
echo "  3. Run 'make benchmark-uniform' to test performance"
echo ""