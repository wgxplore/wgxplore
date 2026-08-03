# wgxplore — build entry point. Two binaries from one tree, mirroring
# zxplore: `wgx` (GUI+TUI, cgo) and `wgx-tui` (static, terminal only).
#
#   make            build both
#   make install    into $(PREFIX)/bin + launcher + icon
#
# .buildnum is a local, gitignored counter bumped once per make run and
# stamped in via -X main.buildNum, so --version reads "0.1.0 b7" and any
# installed binary can be traced to a build.
PREFIX  ?= /usr/local
DESTDIR ?=
BINDIR   = $(DESTDIR)$(PREFIX)/bin
GO      ?= go
GOFLAGS ?= -trimpath

BUILDNUM_FILE = .buildnum
STAMP = -ldflags "-X main.buildNum=$$(cat $(BUILDNUM_FILE) 2>/dev/null || echo 0)"

# `bump` is written first for readability, but a bare `make` must build —
# without this line the default goal was bump and `make` did nothing.
.DEFAULT_GOAL := build

bump:
	@n=$$(cat $(BUILDNUM_FILE) 2>/dev/null || echo 0); echo $$((n + 1)) > $(BUILDNUM_FILE)

build: wgx wgx-tui

wgx: bump $(wildcard *.go) go.mod go.sum
	CGO_ENABLED=1 $(GO) build $(GOFLAGS) $(STAMP) -tags gui -o wgx .

wgx-tui: bump $(wildcard *.go) go.mod go.sum
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) $(STAMP) -o wgx-tui .

test:
	$(GO) vet ./...
	$(GO) vet -tags gui ./...

install: build
	install -d $(BINDIR)
	install -m 0755 wgx     $(BINDIR)/wgx
	install -m 0755 wgx-tui $(BINDIR)/wgx-tui

clean:
	rm -f wgx wgx-tui

.PHONY: bump build test install clean
