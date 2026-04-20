BINARY  := roost
SRC_DIR := src
INSTALL_DIR := $(HOME)/.local/bin

.PHONY: build build-experimental install clean vet lint

build:
	cd $(SRC_DIR) && go build -o ../$(BINARY) .

build-experimental:
	cd $(SRC_DIR) && go build -tags experimental -o ../$(BINARY) .

install: build
	install -d $(INSTALL_DIR)
	install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)

vet:
	cd $(SRC_DIR) && go vet ./...

lint:
	cd $(SRC_DIR) && go tool golangci-lint run ./...

clean:
	rm -f $(BINARY)
