APP_NAME := clash-gen
BUILD_DIR := bin

.PHONY: tidy build test run clean

tidy:
	go mod tidy

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/clash-gen

test:
	go test -race ./...

run:
	BUTTERFLY_CONFIG_TYPE=file \
	BUTTERFLY_CONFIG_FILE_PATH=./config.yaml \
	BUTTERFLY_TRACING_DISABLE=true \
	go run ./cmd/clash-gen

clean:
	rm -rf $(BUILD_DIR)
