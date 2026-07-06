VERSION := $(shell cat VERSION)
BIN := recondeps
LDFLAGS := -s -w -X 'main.VERSION=$(VERSION)'

.PHONY: build install clean test fixtures vet fmt release-patch release-minor release-major

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .
	@echo "built $(BIN) v$(VERSION)"

install: build
	@mkdir -p $(HOME)/bin
	cp ./$(BIN) $(HOME)/bin/$(BIN)
	@echo "installed to $(HOME)/bin/$(BIN)"

vet:
	go vet ./...

fmt:
	gofmt -w .

fixtures:
	python3 tests/gen_fixtures.py

# End-to-end test: serve fixtures, scan every level, assert expectations.
test: build fixtures
	./tests/e2e.sh

clean:
	rm -f $(BIN)
	rm -rf tests/www recondeps_20*

# Semantic version bumps. Usage: make release-patch / release-minor / release-major
release-patch:
	@./bump.sh patch
release-minor:
	@./bump.sh minor
release-major:
	@./bump.sh major

# Real-world-scale stress test (needs network; downloads real libraries).
test-hard: build
	./tests/hardcore.sh

# L10 adversarial suite (dead-map fallback, deep chunks, internal registries).
test-l10: build
	./tests/e2e_l10.sh
