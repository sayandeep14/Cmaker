BINARY      := cmaker
INSTALL_DIR := /usr/local/bin
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# GitHubClientID is cmaker packs' OAuth App client ID for 'cmaker login's
# device flow - a public identifier by GitHub's own design (device flow
# needs no client secret), safe to bake into every build the same way
# main.version is, so 'cmaker login' works out of the box.
GITHUB_CLIENT_ID := Ov23liv0mQqsgNEcXkWZ
LDFLAGS     := -s -w -X main.version=$(VERSION) -X cmaker/internal/packclient.GitHubClientID=$(GITHUB_CLIENT_ID)

.PHONY: build install uninstall test clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

# Rebuilds from the current working tree and installs over whatever's on
# PATH at $(INSTALL_DIR) - the single command to run after pulling/making
# changes so `cmaker` (no path prefix) always reflects this checkout,
# instead of a stale binary silently drifting out of sync with the source.
install: build
	sudo install -m 0755 $(BINARY) $(INSTALL_DIR)/$(BINARY)

uninstall:
	sudo rm -f $(INSTALL_DIR)/$(BINARY)

test:
	go test ./...

clean:
	rm -f $(BINARY)
