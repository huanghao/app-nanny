# app-nanny build tasks

# Build binary
build:
    go build -o nanny .

# Run all tests
test:
    go test ./...

# Run tests with verbose output
test-v:
    go test -v ./...

# Build and install to ~/bin
install: build
    cp nanny ~/bin/nanny

# Clean build artifacts
clean:
    rm -f nanny
