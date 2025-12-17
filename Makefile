.PHONY: build run clean install

BINARY_NAME=fizzy-webhook-proxy
BUILD_DIR=bin

build:
	@echo "Building $(BINARY_NAME)..."
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME) main.go
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

run:
	go run main.go

install: build
	@echo "Installing to /usr/local/bin..."
	cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	chmod +x /usr/local/bin/$(BINARY_NAME)
	@echo "Done. Don't forget to configure /etc/default/$(BINARY_NAME)"

clean:
	rm -rf $(BUILD_DIR)
