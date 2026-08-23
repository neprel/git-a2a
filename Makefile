.PHONY: build test snapshot pilot docs-check skills-check installers-check site-check site-publish

build:
	CGO_ENABLED=0 go build -trimpath -o bin/git-a2a ./cmd/git-a2a

test:
	go test ./...

snapshot:
	goreleaser release --snapshot --clean

pilot:
	./scripts/pilot.sh

docs-check:
	python3 tools/gen-reference.py --check
	python3 tools/sync-skill.py --check

skills-check:
	python3 tools/sync-skill.py --check
	skills-ref validate skills/git-a2a

installers-check:
	bash -n install.sh
	cmp install.sh sites/git-a2a.com/install.sh
	GIT_A2A_TEST_OS=linux GIT_A2A_TEST_ARCH=x86_64 ./install.sh --version 1.0.0 --dir /tmp/git-a2a-dry-run --dry-run
	GIT_A2A_TEST_OS=darwin GIT_A2A_TEST_ARCH=arm64 ./install.sh --version v1.0.0 --dir /tmp/git-a2a-dry-run --dry-run

site-check: installers-check
	python3 sites/git-a2a.com/tools/site_check.py
	@if [ "$${SITE_BROWSER:-0}" = 1 ]; then ./scripts/site-browser-check.sh; fi

site-publish: site-check
	./scripts/site-publish.sh
