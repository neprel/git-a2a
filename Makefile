.PHONY: build test snapshot pilot

build:
	CGO_ENABLED=0 go build -trimpath -o bin/git-a2a ./cmd/git-a2a

test:
	go test ./...

snapshot:
	goreleaser release --snapshot --clean

pilot:
	./scripts/pilot.sh
