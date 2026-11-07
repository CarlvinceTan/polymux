.PHONY: build test run tools

build:
	../../scripts/build.sh server

tools:
	swift build --product media-text --package-path ../..

test:
	../../scripts/test.sh server -race ./...

run:
	../../scripts/run.sh --server
