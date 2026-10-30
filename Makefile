.PHONY: fmt vet test test-integration build desktop

fmt:
	gofmt -w cmd internal
	cd desktop && gofmt -w .

vet:
	go vet ./...
	cd desktop && go vet ./...

test:
	go test -race ./...
	cd desktop && go test -race ./...

test-integration:
	test -n "$$WHALECHIME_TEST_DATABASE_URL"
	go test -race -count=1 ./...

build:
	go build -trimpath -o bin/whalechime ./cmd/whalechime
	go build -trimpath -o bin/whalechime-agent ./cmd/whalechime-agent

desktop:
	cd desktop && go build -trimpath -o ../bin/whalechime-tray .
