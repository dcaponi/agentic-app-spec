# frozen_string_literal: true

require_relative "agentic_engine/types"
require_relative "agentic_engine/logger"
require_relative "agentic_engine/loader"
require_relative "agentic_engine/resolver"
require_relative "agentic_engine/llm"
require_relative "agentic_engine/runner"
require_relative "agentic_engine/orchestrator"

# AgenticEngine is the runtime that powers workflows and agents defined by the
# Agentic App Spec.  Install via `gem install agentic_engine` and use the
# top-level module methods below to orchestrate workflows, invoke agents, and
# register deterministic handlers.
#
#   require "agentic_engine"
#
#   AgenticEngine.register_handler("product_fetch", ->(input) { ... })
#   result = AgenticEngine.orchestrate("product-review", { product_id: 42 })
#
module AgenticEngine
  class << self
    # Run a complete workflow by name.
    #
    # @param workflow_name [String] matches workflows/<name>.yaml
    # @param input [Hash] workflow-level input parameters
    # @return [Hash] WorkflowEnvelope (JSON-serializable)
    def orchestrate(workflow_name, input)
      Orchestrator.orchestrate(workflow_name, input)
    end

    # Invoke a single agent by its directory-name ID.
    #
    # @param agent_id [String] e.g. "review-analyzer"
    # @param input [Hash] resolved input parameters
    # @param config_overrides [Hash, nil] optional model/temperature overrides
    # @return [Hash] { output:, metrics: }
    def invoke_agent(agent_id, input, config_overrides: nil)
      Runner.invoke_agent(agent_id, input, config_overrides: config_overrides)
    end

    # Register a deterministic handler.
    #
    # @param name [String] handler name matching the agent.yaml handler field
    # @param callable [#call] a Proc/lambda receiving input Hash, returning
    #   output Hash (or { output:, metrics: } Hash)
    def register_handler(name, callable)
      Runner.register_handler(name, callable)
    end

    # Register (preload) a JSON schema by name.  This is optional -- schemas
    # are loaded on demand from schemas/<name>.json -- but can be used to
    # inject schemas programmatically.
    #
    # @param name [String] schema name
    # @param schema [Hash] the full schema wrapper with "name", "strict", "schema" keys
    def register_schema(name, schema)
      Loader.schemas[name] = schema
    end
  end
end
