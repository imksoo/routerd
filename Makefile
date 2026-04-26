.PHONY: test build validate-example dry-run-example

test:
	go test ./...

build:
	go build ./cmd/routerd

validate-example:
	go run ./cmd/routerd validate --config examples/basic-static.yaml

dry-run-example:
	go run ./cmd/routerd reconcile --config examples/basic-static.yaml --once --dry-run
