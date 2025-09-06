#!/bin/bash
# Run all torture tests in containerized environments

set -e

# Build the torture test image
echo "Building torture test container..."
docker build -f docker/Dockerfile.torture -t mtlog-audit-torture:latest .

# Function to run a specific torture test scenario
run_scenario() {
    local scenario=$1
    local extra_args=$2
    
    echo ""
    echo "=========================================="
    echo "Running torture test: $scenario"
    echo "=========================================="
    
    case $scenario in
        diskfull)
            # Run with limited disk space using tmpfs
            docker run --rm \
                --name mtlog-torture-$scenario \
                --mount type=tmpfs,destination=/test,tmpfs-size=104857600 \
                -e TMPDIR=/test \
                mtlog-audit-torture:latest \
                /app/torture torture --iterations 100
            ;;
        *)
            # Run other scenarios with normal disk
            docker run --rm \
                --name mtlog-torture-$scenario \
                -v mtlog-torture-data:/test \
                -e TMPDIR=/test \
                mtlog-audit-torture:latest \
                /app/torture torture --iterations $COUNT
            ;;
    esac
    
    if [ $? -eq 0 ]; then
        echo "✓ $scenario test passed"
    else
        echo "✗ $scenario test failed"
        exit 1
    fi
}

# Parse command line arguments
SCENARIO=${1:-all}
COUNT=${2:-1000}

if [ "$SCENARIO" = "all" ]; then
    # Run all scenarios
    scenarios=(kill9 diskfull corruption network_partition clock_skew concurrent_writes recovery panic)
    for s in "${scenarios[@]}"; do
        run_scenario $s "--count $COUNT"
    done
    echo ""
    echo "=========================================="
    echo "✓ All torture tests passed!"
    echo "=========================================="
else
    # Run specific scenario
    run_scenario $SCENARIO "--count $COUNT"
fi

# Cleanup
docker volume rm mtlog-torture-data 2>/dev/null || true