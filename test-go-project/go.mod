module test-go-project

go 1.23.4

require github.com/dcaponi/agentic-app-spec/runtime/go v0.0.0

require gopkg.in/yaml.v3 v3.0.1 // indirect

replace github.com/dcaponi/agentic-app-spec/runtime/go => ../runtime/go
