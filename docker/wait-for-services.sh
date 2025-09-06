#!/bin/bash
# wait-for-services.sh - Wait for all test services to be ready

set -e

echo "Waiting for services to be ready..."

# Wait for MinIO
echo -n "Waiting for MinIO..."
for i in {1..30}; do
    if curl -f http://localhost:9000/minio/health/live > /dev/null 2>&1; then
        echo " ✓"
        break
    fi
    echo -n "."
    sleep 2
done

# Wait for LocalStack (optional)
if [ "${USE_LOCALSTACK:-false}" = "true" ]; then
    echo -n "Waiting for LocalStack..."
    for i in {1..30}; do
        if curl -f http://localhost:4566/_localstack/health > /dev/null 2>&1; then
            echo " ✓"
            break
        fi
        echo -n "."
        sleep 2
    done
fi

# Wait for Azurite (403 means it's running but needs auth)
echo -n "Waiting for Azurite..."
for i in {1..30}; do
    response=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:10000/devstoreaccount1?comp=list 2>/dev/null)
    if [ "$response" = "403" ] || [ "$response" = "200" ]; then
        echo " ✓"
        break
    fi
    echo -n "."
    sleep 2
done

# Wait for Fake GCS (may return 401 unauthorized or 200)
echo -n "Waiting for Fake GCS..."
for i in {1..30}; do
    response=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:4443/storage/v1/b 2>/dev/null)
    if [ "$response" = "200" ] || [ "$response" = "401" ] || [ "$response" = "403" ]; then
        echo " ✓"
        break
    fi
    echo -n "."
    sleep 2
done

echo "All services are ready!"