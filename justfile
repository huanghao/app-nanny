# app-nanny build tasks

# Version string: git tag if available, else short commit hash
_version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
_commit  := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`
_ldflags := "-X main.version=" + _version + " -X main.commit=" + _commit

# Build binary with version info embedded
build:
    go build -ldflags "{{_ldflags}}" -o nanny .

# Run all tests
test:
    go test ./...

# Run tests with verbose output
test-v:
    go test -v ./...

# Build and install to ~/bin
install: build
    mkdir -p ~/bin
    if [ "$(realpath nanny)" != "$(realpath ~/bin/nanny 2>/dev/null || true)" ]; then cp nanny ~/bin/nanny; fi

# Rebuild, install, and restart the daemon so embedded web assets take effect.
restart-daemon: install
    nanny daemon stop
    sleep 1
    nanny daemon start
    sleep 1
    nanny daemon status

# Clean build artifacts
clean:
    rm -f nanny
