APP_NAME := localdrop
BIN_DIR := bin
DIST_DIR := dist

.PHONY: bootstrap dev build build-host build-android-arm64 build-all test clean

bootstrap:
	npm --prefix web install
	go mod tidy

dev:
	./scripts/dev.sh

build:
	$(MAKE) build-host

build-host:
	npm --prefix web run build
	mkdir -p $(BIN_DIR)
	./scripts/build.sh host "$(BIN_DIR)/$(APP_NAME)"

build-android-arm64:
	npm --prefix web run build
	mkdir -p $(DIST_DIR)/android-arm64-termux
	./scripts/build.sh android-arm64 "$(DIST_DIR)/android-arm64-termux/$(APP_NAME)"

build-all: build-host build-android-arm64

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR)
	rm -rf $(DIST_DIR)
