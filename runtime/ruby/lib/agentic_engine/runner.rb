# frozen_string_literal: true

module AgenticEngine
  # Executes a single agent (LLM or deterministic) and returns an AgentResult.
  # Maintains the handler registry for deterministic agents.
  module Runner
    class RunnerError < StandardError; end

    @handlers = {} # { "handler_name" => callable }
    @log = Logger.create("runner")

    class << self
      attr_reader :handlers

      # Register a deterministic handler.
      #
      # @param name [String] handler name (must match agent.yaml handler field)
      # @param callable [#call] a Proc, lambda, or any object responding to #call.
      #   Receives a single Hash argument (the resolved input) and must return a
      #   Hash with :output and :metrics keys (or just :output).
      def register_handler(name, callable)
        @log.info("register_handler", { name: name })
        @handlers[name] = callable
      end

      # Reset handler registry (useful in tests).
      def reset_handlers!
        @handlers.clear
      end

      # Execute an agent given its definition and resolved input hash.
      #
      # @param input [Hash] resolved input parameters
      # @param agent_def [AgentDefinition]
      # @param config_overrides [Hash, nil] optional overrides for model/temperature
      # @return [Hash] { output:, metrics: } (plain hashes for JSON compat)
      def execute_agent(input, agent_def, config_overrides: nil)
        case agent_def.type
        when "llm"
          execute_llm_agent(input, agent_def, config_overrides: config_overrides)
        when "deterministic"
          execute_deterministic_agent(input, agent_def)
        else
          raise RunnerError, "Unknown agent type: #{agent_def.type}"
        end
      end

      # Convenience method: load an agent by ID and execute it.
      #
      # @param agent_id [String] agent directory name
      # @param input [Hash] resolved input parameters
      # @param config_overrides [Hash, nil]
      # @return [Hash] { output:, metrics: }
      def invoke_agent(agent_id, input, config_overrides: nil)
        agent_def = Loader.load_agent(agent_id)
        execute_agent(input, agent_def, config_overrides: config_overrides)
      end

      private

      def execute_llm_agent(input, agent_def, config_overrides: nil)
        model = (config_overrides && config_overrides["model"]) || agent_def.model
        temperature = (config_overrides && config_overrides["temperature"]) || agent_def.temperature || 0.7

        raise RunnerError, "LLM agent '#{agent_def.name}' has no model configured" unless model

        # Build user content from template + input
        user_content = if agent_def.user_message
                         Resolver.resolve_template(agent_def.user_message, input)
                       else
                         JSON.generate(input)
                       end

        @log.debug("execute_llm_agent", { agent: agent_def.name, model: model })

        LLM.call_llm(
          model:         model,
          system_prompt: agent_def.prompt,
          user_content:  user_content,
          temperature:   temperature,
          schema_name:   agent_def.schema
        )
      end

      def execute_deterministic_agent(input, agent_def)
        handler_name = agent_def.handler
        raise RunnerError, "Deterministic agent '#{agent_def.name}' has no handler" unless handler_name

        callable = @handlers[handler_name]
        raise RunnerError, "No handler registered for '#{handler_name}'. " \
                           "Call AgenticEngine.register_handler('#{handler_name}', your_proc) first." unless callable

        @log.debug("execute_deterministic_agent", { agent: agent_def.name, handler: handler_name })

        start_time = Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond)
        result = callable.call(input)
        end_time = Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond)

        # Normalize the result: handler can return a plain hash (treated as
        # output) or a hash with :output/:metrics keys.
        if result.is_a?(Hash) && (result.key?(:output) || result.key?("output"))
          output  = result[:output] || result["output"]
          metrics = result[:metrics] || result["metrics"] || {
            latency_ms: (end_time - start_time).to_i,
            input_tokens: 0,
            output_tokens: 0
          }
        else
          output  = result
          metrics = {
            latency_ms: (end_time - start_time).to_i,
            input_tokens: 0,
            output_tokens: 0
          }
        end

        { output: output, metrics: metrics }
      end
    end
  end
end
