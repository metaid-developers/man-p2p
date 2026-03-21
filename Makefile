BINARY  := man-p2p
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X man-p2p/common.Version=$(VERSION) -s -w"
DIST    := dist

.PHONY: all clean test alpha-test

all: build-darwin-arm64 build-darwin-amd64 build-windows-amd64 build-linux-amd64

build-darwin-arm64:
	@mkdir -p $(DIST)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 \
	  go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-arm64 .

build-darwin-amd64:
	@mkdir -p $(DIST)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 \
	  go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-amd64 .

build-windows-amd64:
	@mkdir -p $(DIST)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
	  go build $(LDFLAGS) -o $(DIST)/$(BINARY)-win32-x64.exe .

build-linux-amd64:
	@mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
	  go build $(LDFLAGS) -o $(DIST)/$(BINARY)-linux-x64 .

test:
	CGO_ENABLED=0 go test ./p2p/... -v -count=1 -timeout 60s

alpha-test:
	CGO_ENABLED=0 go test ./p2p -run 'TestAlphaDualInstanceRealtimeSync|TestAlphaDualProcessRealtimeSync|TestLoadConfig|TestReloadConfig|TestChainSourceDefaultsToEnabled|TestLoadConfigCanDisableChainSource|TestPublishPinWithoutInitializedHost|TestInitHost|TestStorageLimitEnforcement|TestBlocklistOverridesAllowlist|TestSelectivePathMatch|TestOversizedPinStillPassesFilter|TestSelfMode|TestLoadOwnAddressesForSelfMode|TestSelfModeRequiresConfiguredOwnAddress|TestSelfModeBlockOverridesOwnAddress|TestBlockedPath|TestContentPull' -v -count=1 -timeout 60s
	CGO_ENABLED=0 go test ./api -run 'TestP2PStatusEndpoint|TestP2PPeersEndpoint|TestConfigReloadEndpoint|TestAlphaPinMissReturnsNon2xx|TestAlphaContentMissReturnsNon2xx|TestAlphaMetadataOnlyContentContract|TestAlphaP2PStatusFields|TestAlphaConfigReloadUpdatesRuntimeFilterState' -v -count=1 -timeout 60s
	CGO_ENABLED=0 go test ./man -run 'TestChatPubKeyParsed|TestIngestP2PPinStoresPinAndMetaIdInfo|TestInitRuntimeWithoutChainSourceSkipsAdapters' -v -count=1 -timeout 60s

clean:
	rm -rf $(DIST)
