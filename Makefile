.PHONY: build install clean test fmt

BINARY := pr-view
OUTPUT := bin/$(BINARY)

build:
	@echo "Building $(BINARY)..."
	@mkdir -p bin
	go build -v -o $(OUTPUT) .

install: build
	@echo "Installing $(BINARY)..."
	@if [ -n "$$GOBIN" ]; then \
		mkdir -p "$$GOBIN" && install -m 0755 $(OUTPUT) "$$GOBIN"/$(BINARY); \
	else \
		install -m 0755 $(OUTPUT) /usr/local/bin/$(BINARY); \
	fi

clean:
	rm -f $(OUTPUT)

test:
	go test ./...

fmt:
	gofmt -w .
