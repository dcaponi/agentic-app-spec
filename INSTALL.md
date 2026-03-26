# Installing the Agentic App Spec

## CLI

```bash
curl -fsSL https://raw.githubusercontent.com/dcaponi/agentic-app-spec/main/scripts/install.sh | bash
```

Or set a specific version / install directory:

```bash
VERSION=v1.0.0 INSTALL_DIR=~/.local/bin curl -fsSL https://raw.githubusercontent.com/dcaponi/agentic-app-spec/main/scripts/install.sh | bash
```

Or build from source:

```bash
cd cli && cargo build --release
cp target/release/agentic /usr/local/bin/
```

## Runtimes

### TypeScript

```bash
npm install agentic-engine
```

### Python

```bash
pip install "agentic-engine @ git+https://github.com/dcaponi/agentic-app-spec.git#subdirectory=runtime/python"
```

### Ruby

In your Gemfile:

```ruby
gem "agentic_engine", git: "https://github.com/dcaponi/agentic-app-spec.git", glob: "runtime/ruby/*.gemspec"
```

### Go

```bash
go get github.com/dcaponi/agentic-app-spec/runtime/go@latest
```
