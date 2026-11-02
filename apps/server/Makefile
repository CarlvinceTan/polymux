.PHONY: build test run

build:
	mkdir -p ../../dist/server
	go build -tags sqlite_fts5 -o ../../dist/server/polymux ./cmd/polymux

test:
	../../scripts/test.sh server -race ./...

run:
	../../scripts/run.sh --server
