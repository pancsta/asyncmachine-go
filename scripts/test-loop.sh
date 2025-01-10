#!/bin/bash

#set -x

while true; do
    echo "Running tests..."
    task clean
    output=$(env AM_TEST_RUNNER=1 go test ./... -race -v 2>&1) # Run tests and capture output

    if echo "$output" | grep -q "WARNING: DATA RACE"; then
        echo "Race condition detected!"
        echo "$output"                     # Print output for debugging
        exit 1
    fi

    if echo "$output" | grep -q "FAIL"; then
        echo "Failure detected!"
        echo "$output"                     # Print output for debugging
        exit 1
    fi

#    echo "No race detected. Re-running tests..."
done