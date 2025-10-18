.PHONY: all
all: install generate-example run-example

.PHONY: generate-example
generate-example:
	go run ./cmd/assemble/main.go -pkg ./example/core.go -var Core -o ./example/core_assemble_gen.go

.PHONY: generate
generate:
	go generate ./...

.PHONY: run-example
run-example:
	go run ./example

.PHONY: install
install:
	go install ./cmd/assemble

.PHONY: lint
lint:
	golangci-lint run ./...