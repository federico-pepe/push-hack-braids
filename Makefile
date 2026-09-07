BINARY    := push-braids
REPO_ROOT := $(abspath $(CURDIR))
DOCKER    := docker run --rm --platform linux/amd64 \
               -v "$(REPO_ROOT)":/work -w /work/src \
               golang:1.25-bookworm

BRAIDS_DIR := third_party/braids
BRAIDS_SRCS := $(BRAIDS_DIR)/dsp/braids/macro_oscillator.cc \
               $(BRAIDS_DIR)/dsp/braids/analog_oscillator.cc \
               $(BRAIDS_DIR)/dsp/braids/digital_oscillator.cc \
               $(BRAIDS_DIR)/dsp/braids/resources.cc \
               $(BRAIDS_DIR)/dsp/braids/quantizer.cc \
               $(BRAIDS_DIR)/dsp/stmlib/utils/random.cc \
               $(BRAIDS_DIR)/dsp/braids_plugin.cpp

.PHONY: all build build-dsp clean

all: build build-dsp

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

# The DSP plugin (third_party/braids/, vendored — see its own
# THIRD_PARTY_LICENSES.md) is portable C++ with no ARM-specific code, so
# it builds native x86_64 in a plain Debian container — no cross toolchain
# needed, same as the Go binary's own build.
build-dsp:
	@echo "Building dsp.so (native x86_64, via Docker)..."
	@mkdir -p build
	docker run --rm --platform linux/amd64 -v "$(REPO_ROOT)":/work -w /work debian:bookworm sh -c '\
	  apt-get update -qq && apt-get install -qq -y g++ >/dev/null && \
	  for s in $(BRAIDS_SRCS); do \
	    g++ -O3 -fPIC -std=c++14 -DTEST -I$(BRAIDS_DIR)/dsp -c "$$s" -o "build/$$(basename "$$s" | sed "s/\.[^.]*$$/.o/")"; \
	  done && \
	  g++ -shared build/*.o -o dsp.so -lm'
	@echo "Done: dsp.so"

clean:
	rm -f $(BINARY) dsp.so
	rm -rf build
