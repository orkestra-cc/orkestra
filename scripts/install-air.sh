#!/bin/bash

# Pinned, not @latest: a release that raises its `go` directive above the
# project's toolchain (go.mod / .mise.toml / CI) refuses to build, as v1.67.2
# did on Go 1.25. Bump together with the Go version.
AIR_VERSION="${AIR_VERSION:-v1.67.4}"

# Install air if not already installed
if ! command -v air &> /dev/null && ! command -v ~/go/bin/air &> /dev/null; then
    echo "Installing air for hot reload..."
    go install "github.com/air-verse/air@${AIR_VERSION}"
    echo "Air installed successfully"
fi

# Find air binary
if command -v air &> /dev/null; then
    AIR_BIN="air"
elif [ -f ~/go/bin/air ]; then
    AIR_BIN="~/go/bin/air"
elif [ -f "$(go env GOPATH)/bin/air" ]; then
    AIR_BIN="$(go env GOPATH)/bin/air"
else
    echo "Error: air not found. Please ensure Go is properly installed."
    exit 1
fi

echo "Starting with air..."
eval $AIR_BIN
