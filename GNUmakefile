# GNU make reads this file before Makefile. It adds targets without editing Makefile (which another change is
# editing right now); fold it into Makefile when convenient. All Makefile targets keep working.
include Makefile

VERSION ?= dev

# Static Linux binaries for the Docker image's platforms, built without cgo exactly as the Dockerfile does.
# This is the offline part of "does the image build": it needs no Docker daemon and no network.
.PHONY: build-linux
build-linux:
	@set -e; cd $(SERVER); mkdir -p bin; \
	for arch in amd64 arm64; do \
		out=bin/calendard-linux-$$arch; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $$out ./cmd/calendard; \
		go version -m $$out | grep -q 'CGO_ENABLED=0' || { echo "$$out was built with cgo"; exit 1; }; \
		head -c4 $$out | od -An -c | grep -q 'E *L *F' || { echo "$$out is not an ELF binary"; exit 1; }; \
		printf '%s  %s bytes\n' $$out $$(wc -c < $$out | tr -d ' '); \
	done
