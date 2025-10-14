# assemble — Pragmatic Dependency Injection for Go

**assemble** is a pragmatic, lightweight dependency injection (DI) library for Go.  
It focuses on **clarity**, **type safety**, and **zero reflection** through a combination of runtime and generated dependency graphs.

[![Go Build](https://github.com/xfrr/assemble/actions/workflows/go_build.yaml/badge.svg)](https://github.com/xfrr/assemble/actions/workflows/go_build.yaml)
[![Go Lint](https://github.com/xfrr/assemble/actions/workflows/go_lint.yaml/badge.svg)](https://github.com/xfrr/assemble/actions/workflows/go_lint.yaml)
---

## Key Features

- 🧩 **Strongly typed DI container** — safe, reflection-free dependency resolution  
- ⚙️ **Static code generation**: compile-time wiring for performance and safety  
- 🔄 **Lifecycle hooks**: `OnStart`, `OnStop`, plus type-aware `OnStartFor[T]` / `OnStopFor[T]`  
- 🧠 **Deterministic startup and shutdown** based on creation order and priorities  
- ⏱️ **Timeouts and multi-error aggregation** for clean lifecycle management  
- 🧭 **Graph export** (DOT / PlantUML) for visualizing dependency and shutdown order  
- 🧰 Designed for **modular services**, **CLIs**, and **testable applications**

## Installation

To use and import the library, run:

```bash
go get github.com/xfrr/assemble
```

To install the generator tool, run:

```bash
go install github.com/xfrr/assemble/cmd/assemblegen@latest
```

## Usage

Check the [example](./example/main.go) for a complete usage demonstration.

## License

MIT License. See [LICENSE](./LICENSE) for details.
