# Docker Test Infrastructure

This directory contains Docker Compose configurations for running integration tests with real storage backends.

## Services

### MinIO (S3-compatible storage)
- **Port**: 9000 (API), 9001 (Console)
- **Credentials**: minioadmin/minioadmin
- **Features**: 
  - Server-side encryption (SSE-S3)
  - Versioning
  - Object Lock (COMPLIANCE mode)
  - Lifecycle policies
  - Pre-configured compliance buckets

### LocalStack (Optional AWS services)
- **Port**: 4566
- **Services**: S3, KMS, STS
- **Credentials**: test/test

### Azurite (Azure Blob Storage emulator)
- **Port**: 10000 (Blob), 10001 (Queue), 10002 (Table)
- **Connection String**: See `.env` file

### Fake GCS (Google Cloud Storage emulator)
- **Port**: 4443
- **Mode**: HTTP (not HTTPS)

## Usage

### Start all services
```bash
make docker-up
```

### Stop all services
```bash
make docker-down
```

### Run integration tests with Docker
```bash
make docker-test
```

### Run specific backend tests
```bash
# Start services first
make docker-up

# Run S3 compliance tests
go test -tags=integration -run TestS3BackendCompliance ./integration/...

# Run HIPAA workflow tests
go test -tags=integration -run TestHIPAACompliantWorkflow ./integration/...

# Clean up
make docker-down
```

## Pre-configured Buckets

The following buckets are automatically created with compliance settings:

| Bucket | Compliance | Features |
|--------|------------|----------|
| `hipaa-audit-test` | HIPAA | Encryption, Versioning, 6-year retention |
| `sox-audit-test` | SOX | Encryption, Versioning, 7-year retention |
| `pci-audit-test` | PCI-DSS | Encryption, 1-year retention |
| `gdpr-audit-test` | GDPR | Encryption, 3-year lifecycle |
| `mtlog-audit-test` | General | Basic testing bucket |

## Environment Variables

See `docker/.env` for configuration. Key variables:

- `MINIO_ENDPOINT`: MinIO API endpoint
- `MINIO_ACCESS_KEY`: MinIO access key
- `MINIO_SECRET_KEY`: MinIO secret key
- `AZURITE_CONNECTION_STRING`: Azure Storage connection string
- `GCS_EMULATOR_HOST`: GCS emulator host

## Troubleshooting

### Services won't start
- Check if ports are already in use: `lsof -i :9000`
- Ensure Docker is running: `docker ps`
- Check logs: `docker-compose -f docker/docker-compose.yml logs <service>`

### MinIO Object Lock not working
- Object Lock must be enabled when bucket is created
- Use the pre-configured buckets or create with: `mc mb --with-lock`

### Tests can't connect to services
- Ensure services are healthy: `docker-compose -f docker/docker-compose.yml ps`
- Check network connectivity: `curl http://localhost:9000/minio/health/live`
- Verify environment variables are loaded: `source docker/.env`

## Torture Testing

The torture tests verify that the audit system cannot lose data under extreme conditions.

### Building the Torture Test Container

```bash
# Build the torture test container
docker build -f docker/Dockerfile.torture -t mtlog-audit-torture:latest .
```

### Running Torture Tests

#### Disk Full Test
Tests the system's behavior when disk space is exhausted:

```bash
# Quick test (10 iterations)
make torture-docker-diskfull

# Or manually with custom iterations
docker run --rm \
  --mount type=tmpfs,destination=/test,tmpfs-size=104857600 \
  -e TMPDIR=/test \
  mtlog-audit-torture:latest \
  /app/torture torture --iterations 100 --scenario diskfull --verbose
```

This test:
- Runs in a container with only 100MB of disk space (tmpfs)
- Actually fills the disk by writing real data until ENOSPC
- Verifies the WAL handles disk full conditions gracefully
- Ensures no data loss even when disk is exhausted

#### All Torture Scenarios
```bash
# Run all torture tests (1000 iterations)
make torture-docker

# Run full suite (1M iterations - takes ~24 hours)
make torture-docker-full
```

Available scenarios:
- `diskfull`: Disk exhaustion during writes
- `kill9`: Process termination (kill -9) during writes
- `corruption`: Random bit flips and data corruption
- `all`: Run all scenarios

### Container Details

The torture test container:
- Uses `golang:1.25-alpine` for building
- Runs on `alpine:3.20` for minimal footprint
- Includes utilities for filesystem operations
- Respects `TMPDIR` environment variable for test directory

## Production Considerations

This setup is for **testing only**. For production:

1. Use real AWS S3, Azure Blob Storage, or Google Cloud Storage
2. Configure proper IAM roles and policies
3. Enable MFA for sensitive operations
4. Use KMS for encryption keys
5. Configure VPC endpoints for private connectivity
6. Enable access logging and CloudTrail/Activity Logs
7. Set up cross-region replication for disaster recovery