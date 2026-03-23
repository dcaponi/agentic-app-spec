# frozen_string_literal: true

Gem::Specification.new do |spec|
  spec.name          = "agentic_engine"
  spec.version       = "0.1.0"
  spec.authors       = ["Agentic App Spec"]
  spec.email         = ["hello@agentic.dev"]

  spec.summary       = "Runtime engine for Agentic App Spec workflows and agents"
  spec.description   = "Executes LLM and deterministic agents defined by the Agentic App Spec. " \
                        "Provides workflow orchestration with parallel execution, retry, fallback, " \
                        "and short-circuit support."
  spec.homepage      = "https://github.com/agentic-app-spec/agentic-app-spec"
  spec.license       = "MIT"

  spec.required_ruby_version = ">= 3.1"

  spec.files         = Dir["lib/**/*.rb"]
  spec.require_paths = ["lib"]

  spec.add_dependency "ruby-openai", "~> 7.0"
  spec.add_dependency "concurrent-ruby", "~> 1.2"
end
