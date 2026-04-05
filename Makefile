BINARY  := roost
SRC_DIR := src
INSTALL_DIR := $(HOME)/.local/bin

.PHONY: build install clean vet

build:
	cd $(SRC_DIR) && go build -o ../$(BINARY) .

install: build
	install -d $(INSTALL_DIR)
	install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)

vet:
	cd $(SRC_DIR) && go vet ./...

clean:
	rm -f $(BINARY)
