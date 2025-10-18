.PHONY: all
all: install generate-example-var generate-example-fn generate-bench

.PHONY: generate-example
generate-example-var:
	go run ./cmd/assemble/main.go -pkg ./example/di -var Core -o ./example/di/assemble_core_gen.go

.PHONY: generate-example-fn
generate-example-fn:
	go run ./cmd/assemble/main.go -pkg ./example/di -fn AssembleServer -o ./example/di/assemble_server_gen.go

.PHONY: generate-bench
generate-bench:
	go run ./cmd/assemble/main.go -pkg ./test -var NodeDependencyGraph -o ./test/benchmark_assemble_gen.go

.PHONY: generate
generate:
	go generate ./...

.PHONY: run-example
run-example:
	go run ./example

.PHONY: install
install:
	go install ./cmd/assemble

.PHONY: benchmark
benchmark:
	go test -v -bench=. ./...

.PHONY: lint
lint:
	golangci-lint run ./...