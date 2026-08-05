BINARY      := cmaker
INSTALL_DIR := /usr/local/bin
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)

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
