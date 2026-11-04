.PHONY: fmt vet test build desktop

fmt:
	gofmt -w cmd internal desktop

vet:
	go vet ./...

test:
	go test -race ./...

build:
	go build -trimpath -o bin/whalechime ./desktop
	go build -trimpath -o bin/whalechime-web ./cmd/whalechime

desktop:
	go build -trimpath -o bin/whalechime ./desktop
