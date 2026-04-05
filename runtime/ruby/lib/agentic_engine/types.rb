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

  TrailEntry = Struct.new(
    :step_id,
    :event,
    :timestamp,
    :data,
    keyword_init: true
  ) do
    def to_h
      h = { step_id: step_id, event: event, timestamp: timestamp }
      h[:data] = data unless data.nil?
      h
    end
  end

  StepResult = Struct.new(
    :id,
    :agent,
    :workflow,
    :status,          # "success" | "error" | "not_executed" | "partial_failure"
    :output,
    :metrics,         # StepMetrics
    :attempts,
    :used_fallback,
    :fallback_reason,
    :sub_envelope,    # Hash (workflow envelope for sub-workflows)
    :error,
    keyword_init: true
  ) do
    def to_h
      h = { id: id, status: status, output: output,
            metrics: metrics.is_a?(StepMetrics) ? metrics.to_h : (metrics || {}) }
      h[:agent] = agent unless agent.nil? || agent.empty?
      h[:workflow] = workflow unless workflow.nil? || workflow.empty?
      h[:attempts] = attempts unless attempts.nil?
      h[:used_fallback] = used_fallback unless used_fallback.nil?
      h[:fallback_reason] = fallback_reason unless fallback_reason.nil? || fallback_reason.empty?
      h[:sub_envelope] = sub_envelope unless sub_envelope.nil?
      h[:error] = error unless error.nil?
      h
    end
  end

  WorkflowEnvelope = Struct.new(
    :workflow,
    :version,
    :request_id,
    :status,        # "success" | "error" | "partial_failure"
    :timestamps,    # { started_at:, completed_at: }
    :metrics,       # { total_latency_ms:, total_input_tokens:, ... }
    :steps,         # Array of StepResult
    :trail,         # Array of TrailEntry
    :result,
    :error,
    keyword_init: true
  ) do
    def to_h
      h = {
        workflow: workflow, version: version, request_id: request_id,
        status: status, timestamps: timestamps, metrics: metrics,
        steps: steps.map { |s| s.is_a?(StepResult) ? s.to_h : s },
        trail: trail.map { |t| t.is_a?(TrailEntry) ? t.to_h : t },
        result: result
      }
      h[:error] = error unless error.nil?
      h
    end
  end

  # Wraps a workflow failure with the partial envelope (including trail).
  class WorkflowError < StandardError
    attr_reader :envelope

    def initialize(message, envelope: nil)
      super(message)
      @envelope = envelope
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
    :name, :description, :type, :base_url, :api_key_env, :model,
    :temperature, :input_type, :image_detail, :schema, :user_message,
    :handler, :prompt, :input,
    keyword_init: true
  )

  WorkflowDefinition = Struct.new(
    :name, :description, :version,
    :input,   # { param_name => { "type" => ..., "required" => ... } }
    :steps,   # Array of WorkflowStep, ParallelBlock, LoopBlock, or ForEachBlock
    :output,  # { key => "$.path" }
    keyword_init: true
  )

  # Control flow types
  SwitchNext = Struct.new(:expression, :cases, :default, keyword_init: true)
  IfNext = Struct.new(:condition, :then_target, :else_target, keyword_init: true)
  NextField = Struct.new(:target, :switch, :if_next, keyword_init: true)

  RetryConfig = Struct.new(:max_attempts, :backoff_ms, keyword_init: true)
  FallbackConfig = Struct.new(:agent, :workflow, :config, keyword_init: true)

  WorkflowStep = Struct.new(
    :id, :agent, :workflow, :input, :config,
    :retry_config, :fallback, :requires, :next_field,
    keyword_init: true
  )

  ParallelBranch = Struct.new(
    :id, :agent, :workflow, :input, :config,
    :retry_config, :fallback,
    keyword_init: true
  )

  ParallelBlock = Struct.new(
    :id, :join, :branches, :next_field,
    keyword_init: true
  )

  LoopBlock = Struct.new(
    :id, :agent, :workflow, :input, :config,
    :until_condition, :max_iterations, :retry_config, :fallback, :next_field,
    keyword_init: true
  )

  ForEachBlock = Struct.new(
    :id, :agent, :workflow, :input, :config,
    :collection, :max_concurrency, :retry_config, :fallback, :next_field,
    keyword_init: true
  )
end
