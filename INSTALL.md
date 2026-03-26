# Installing the Agentic App Spec

## CLI

Download the prebuilt binary for your platform from the
[releases](https://github.com/dominickcaponi/agentic-app-spec/releases) page
and put it on your `PATH`.

| Platform       | Binary                    |
|----------------|---------------------------|
| macOS arm64    | `agentic-darwin-arm64`    |
| macOS x86_64   | `agentic-darwin-x86_64`   |
| Linux x86_64   | `agentic-linux-x86_64`    |
| Linux arm64    | `agentic-linux-arm64`     |
| Windows x86_64 | `agentic-windows-x86_64.exe` |

Or build from source:

```bash
cd cli && cargo build --release
# binary at cli/target/release/agentic
```

## Runtimes

### TypeScript

```bash
npm install agentic-engine
```

### Python

```bash
pip install "agentic-engine @ git+https://github.com/dominickcaponi/agentic-app-spec.git#subdirectory=runtime/python"
```

### Ruby

In your Gemfile:

```ruby
gem "agentic_engine", git: "https://github.com/dominickcaponi/agentic-app-spec.git", glob: "runtime/ruby/*.gemspec"
```

### Go

```bash
go get github.com/dominickcaponi/agentic-app-spec/runtime/go@latest
```
