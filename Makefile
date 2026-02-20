.PHONY: test vet lint coverage clean

test:
	go test ./... -race -covermode=atomic -coverprofile=coverage.out

vet:
	go vet ./...

lint:
	@which staticcheck > /dev/null || (echo "Installing staticcheck..." && go install honnef.co/go/tools/cmd/staticcheck@latest)
	staticcheck ./...

coverage:
	go tool cover -html=coverage.out

clean:
	rm -f coverage.out

all: vet lint test
