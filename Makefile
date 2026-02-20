# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GO_VERSION ?= $(shell go version | awk '{print $$3}')

# Ldflags for version injection
LDFLAGS := -X 'github.com/user/go-deploy-guard/sdk.Version=$(VERSION)' \
           -X 'github.com/user/go-deploy-guard/sdk.GitCommit=$(GIT_COMMIT)' \
           -X 'github.com/user/go-deploy-guard/sdk.BuildTime=$(BUILD_TIME)' \
           -X 'github.com/user/go-deploy-guard/sdk.GoVersion=$(GO_VERSION)'

.PHONY: test vet lint coverage clean version

test:
	go test ./... -race -covermode=atomic -coverprofile=coverage.out

vet:
	go vet ./...

lint:
	@which staticcheck > /dev/null || (echo "Installing staticcheck..." && go install honnef.co/go/tools/cmd/staticcheck@latest)
	staticcheck ./...

coverage:
	go tool cover -html=coverage.out

# Show version info
version:
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(GIT_COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Go Version: $(GO_VERSION)"

clean:
	rm -f coverage.out

all: vet lint test
