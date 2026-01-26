#!/bin/sh
# Cloudflared build script for FreeBSD
# Builds the latest version from source

set -e

# Log function
log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $*" | tee -a /var/log/cloudflared-build.log
}

log "Starting cloudflared build from source"

# Ensure Go is available
export PATH=/usr/local/go/bin:$PATH

# Create build directory
BUILD_DIR="/tmp/cloudflared-build"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"
cd "$BUILD_DIR"

# Clone the repository
log "Cloning cloudflared repository"
git clone https://github.com/cloudflare/cloudflared.git .
git checkout $(git tag --sort=-version:refname | head -1)

# Fix build constraints for FreeBSD
log "Fixing build constraints for FreeBSD"
sed -i "" "s/darwin || linux/darwin || linux || freebsd/" diagnostic/network/collector_unix.go
sed -i "" "s/darwin || linux/darwin || linux || freebsd/" diagnostic/network/collector_unix_test.go

# Copy FreeBSD system collector
cp diagnostic/system_collector_linux.go diagnostic/system_collector_freebsd.go
sed -i "" "s/linux/freebsd/" diagnostic/system_collector_freebsd.go

# Build the binary
log "Building cloudflared"
go build -o cloudflared-new ./cmd/cloudflared

# Verify the build
if [ -f "cloudflared-new" ] && ./cloudflared-new --version >/dev/null 2>&1; then
    log "Build successful"
    echo "$BUILD_DIR/cloudflared-new"
else
    log "Build failed"
    exit 1
fi
