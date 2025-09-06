#!/bin/bash
# Run disk full torture test in a containerized environment with limited disk space

set -e

echo "Building torture test container..."
docker build -f docker/Dockerfile.torture -t mtlog-audit-torture:latest .

echo "Running disk full torture test in container with 100MB disk limit..."
docker run --rm \
  --name mtlog-torture-diskfull \
  --mount type=tmpfs,destination=/test,tmpfs-size=104857600 \
  -e TMPDIR=/test \
  mtlog-audit-torture:latest \
  /app/torture torture --iterations 10 --scenario diskfull --verbose

echo "Disk full torture test completed successfully!"