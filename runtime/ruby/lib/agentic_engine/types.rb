# frozen_string_literal: true

module AgenticEngine
  # ── Public types (returned to consumers) ──

  StepMetrics = Struct.new(
    :latency_ms,
    :input_tokens,
    :output_tokens,
    keyword_init: true
  ) do
    def to_h
      { latency_ms: latency_ms, input_tokens: input_tokens, output_tokens: output_tokens }
    end
  end

  StepResult = Struct.new(
    :id,
    :agent,
    :status,        # "success" | "skipped" | "error"
    :output,
    :metrics,       # StepMetrics (or hash)
    :attempts,
    :used_fallback,
    :error,
    keyword_init: true
  ) do
    def to_h
      h = {
        id: id,
        agent: agent,
        status: status,
        output: output,
        metrics: metrics.is_a?(StepMetrics) ? metrics.to_h : metrics
      }
      h[:attempts] = attempts unless attempts.nil?
      h[:used_fallback] = used_fallback unless used_fallback.nil?
      h[:error] = error unless error.nil?
      h
    end
  end

  WorkflowEnvelope = Struct.new(
    :workflow,
    :version,
    :request_id,
    :status,        # "success" | "error" | "short_circuited"
    :timestamps,    # { started_at:, completed_at: }
    :metrics,       # { total_latency_ms:, total_input_tokens:, ... }
    :steps,         # Array of StepResult
    :result,
    :error,
    keyword_init: true
  ) do
    def to_h
      h = {
        workflow: workflow,
        version: version,
        request_id: request_id,
        status: status,
        timestamps: timestamps,
        metrics: metrics,
        steps: steps.map { |s| s.is_a?(StepResult) ? s.to_h : s },
        result: result
      }
      h[:error] = error unless error.nil?
      h
    end
  end

  AgentResult = Struct.new(
    :output,
    :metrics,
    keyword_init: true
  ) do
    def to_h
      { output: output, metrics: metrics.is_a?(StepMetrics) ? metrics.to_h : metrics }
    end
  end

  # ── Engine-internal types ──

  AgentDefinition = Struct.new(
    :name,
    :description,
    :type,           # "llm" | "deterministic"
    :base_url,       # optional OpenAI-compatible base URL
    :api_key_env,    # env var name for the API key
    :model,
    :temperature,
    :input_type,     # "image" | "text"
    :image_detail,   # "low" | "high" | "auto"
    :schema,         # schema name or nil
    :user_message,
    :handler,        # deterministic handler name
    :prompt,         # loaded from prompt.md
    :input,          # { param_name => { "type" => ..., "required" => ... } }
    keyword_init: true
  )

  WorkflowDefinition = Struct.new(
    :name,
    :description,
    :version,
    :input,          # { param_name => { "type" => ..., "required" => ... } }
    :steps,          # Array of WorkflowStep or ParallelGroup hashes
    :output,         # { key => "$.path" }
    keyword_init: true
  )

  WorkflowStep = Struct.new(
    :id,
    :agent,
    :input,          # { param => "$.path" or literal }
    :config,         # optional overrides
    :retry_config,   # { max_attempts:, backoff_ms: }
    :fallback,       # { agent:, config: }
    :short_circuit,  # { condition:, defaults: }
    keyword_init: true
  )

  ParallelGroup = Struct.new(
    :parallel,       # Array of WorkflowStep
    keyword_init: true
  )

  RoutingAgentDefinition = Struct.new(
    :name, :description, :strategy, :base_url, :api_key_env, :model,
    :temperature, :handler, :prompt, :input,
    keyword_init: true
  ) do
    def to_h
      super.compact
    end
  end

  RouteBlock = Struct.new(
    :id, :routing_agent, :input, :routes, :retry, :fallback,
    keyword_init: true
  )

  RouteEntry = Struct.new(:route, keyword_init: true)

  RouteOutput = Struct.new(:route, :router_output, :result, keyword_init: true) do
    def to_h
      { route: route, router_output: router_output, result: result }
    end
  end
end
