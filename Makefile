BINARY  := man-p2p
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X man-p2p/common.Version=$(VERSION) -s -w"
DIST    := dist

.PHONY: all clean test

all: build-darwin-arm64 build-darwin-amd64 build-windows-amd64 build-linux-amd64

build-darwin-arm64:
	@mkdir -p $(DIST)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
	  go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-arm64 .

build-darwin-amd64:
	@mkdir -p $(DIST)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 \
	  go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-amd64 .

build-windows-amd64:
	@mkdir -p $(DIST)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
	  go build $(LDFLAGS) -o $(DIST)/$(BINARY)-win32-x64.exe .

build-linux-amd64:
	@mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
	  go build $(LDFLAGS) -o $(DIST)/$(BINARY)-linux-x64 .

test:
	CGO_ENABLED=0 go test ./p2p/... -v -count=1 -timeout 60s

clean:
	rm -rf $(DIST)
