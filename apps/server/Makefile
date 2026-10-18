.PHONY: build test run tools

build:
	go build -tags sqlite_fts5 -o /tmp/polymux ./cmd/polymux

tools:
	swift build --product media-text --package-path ../..

test:
	go test -tags sqlite_fts5 -race ./...

run:
	go run -tags sqlite_fts5 ./cmd/polymux serve
