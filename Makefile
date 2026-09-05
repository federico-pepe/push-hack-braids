BINARY    := push-braids-host
REPO_ROOT := $(abspath $(CURDIR))
DOCKER    := docker run --rm --platform linux/amd64 \
               -v "$(REPO_ROOT)":/work -w /work/src \
               golang:1.25-bookworm

.PHONY: all build clean

all: build

# cgo (dlopen + libasound) means this can't cross-compile with a plain
# `go build` — build natively inside a linux/amd64 container instead.
build:
	@echo "Building $(BINARY) (cgo, linux/amd64, via Docker)..."
	@# AbletonOS keeps its dynamic linker at /lib/ld-linux-x86-64.so.2, not
	@# /lib64/... the way Debian does; extldflags carries the fix through
	@# cgo's external linker.
	$(DOCKER) sh -c "apt-get update -qq && apt-get install -qq -y libasound2-dev >/dev/null && \
	  CGO_ENABLED=1 go build -ldflags '-s -w -linkmode external -extldflags -Wl,--dynamic-linker=/lib/ld-linux-x86-64.so.2' -o ../$(BINARY) ."
	@echo "Done: $(BINARY)"

clean:
	rm -f $(BINARY)
