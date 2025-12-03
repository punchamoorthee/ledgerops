#!/bin/bash
set -e # Abort on any error

# Colors for readability
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}[1/5] Wiping Environment...${NC}"
docker compose down -v

echo -e "${CYAN}[2/5] Starting Stack...${NC}"
docker compose up -d --wait
echo -e "${GREEN}Infrastructure is healthy.${NC}"

echo -e "${CYAN}[3/5] Seeding Data (1000 Accounts)...${NC}"
# Seeder runs on host, so it uses localhost
go run cmd/seeder/main.go

echo -e "${CYAN}[4/5] Running Benchmarks (k6 via Docker)...${NC}"

# We run k6 inside the docker network. 
# "api" is the hostname of the go service within the network.
echo ">> Test 1: Uniform Load"
docker run --rm -i \
  --network ledgerops_default \
  -v $(pwd)/scripts:/scripts \
  -e BASE_URL=http://api:8080 \
  grafana/k6 run /scripts/workload_uniform.js

echo ">> Test 2: Hot-Spot Contention"
docker run --rm -i \
  --network ledgerops_default \
  -v $(pwd)/scripts:/scripts \
  -e BASE_URL=http://api:8080 \
  grafana/k6 run /scripts/workload_hotspot.js

echo -e "${CYAN}[5/5] Cleanup...${NC}"
# OPTIONAL: Comment out the next line if you want to inspect DB/Grafana after the run
docker compose down -v

echo -e "${GREEN}=== Cycle Complete ===${NC}"