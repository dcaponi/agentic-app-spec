# frozen_string_literal: true

require "securerandom"
require "concurrent"

module AgenticEngine
  # Full workflow orchestration engine.
  #
  # Walks the step list in order, executes serial steps sequentially and
  # parallel groups concurrently (via Concurrent::Promises), handles retry,
  # fallback, and short-circuit logic.
  module Orchestrator
    class OrchestrationError < StandardError; end

    @log = Logger.create("orchestrator")

    class << self
      # Orchestrate a complete workflow.
      #
      # @param workflow_name [String] matches workflows/<name>.yaml
      # @param input [Hash] workflow-level input parameters
      # @return [Hash] WorkflowEnvelope as a plain Hash (JSON-serializable)
      def orchestrate(workflow_name, input)
        started_at = Time.now.utc.iso8601
        request_id = SecureRandom.uuid
        workflow_def = Loader.load_workflow(workflow_name)

        @log.info("orchestrate:start", { workflow: workflow_name, request_id: request_id })

        context = { "input" => stringify_keys(input), "steps" => {} }
        step_results = []
        short_circuited = false
        short_circuit_defaults = nil
        total_metrics = { total_latency_ms: 0, total_input_tokens: 0, total_output_tokens: 0,
                          steps_executed: 0, steps_skipped: 0 }

        workflow_def.steps.each do |entry|
          if short_circuited
            if entry.is_a?(RouteEntry)
              fill_route_skipped(entry.route, short_circuit_defaults, context, step_results)
              total_metrics[:steps_skipped] += 1
            elsif entry.is_a?(ParallelGroup)
              entry.parallel.each do |step|
                default_output = short_circuit_defaults&.dig(step.id) || {}
                result = StepResult.new(
                  id: step.id, agent: step.agent, status: "skipped",
                  output: default_output,
                  metrics: StepMetrics.new(latency_ms: 0, input_tokens: 0, output_tokens: 0)
                )
                step_results << result
                context["steps"][step.id] = { "output" => default_output }
                total_metrics[:steps_skipped] += 1
              end
            else
              default_output = short_circuit_defaults&.dig(entry.id) || {}
              result = StepResult.new(
                id: entry.id, agent: entry.agent, status: "skipped",
                output: default_output,
                metrics: StepMetrics.new(latency_ms: 0, input_tokens: 0, output_tokens: 0)
              )
              step_results << result
              context["steps"][entry.id] = { "output" => default_output }
              total_metrics[:steps_skipped] += 1
            end
            next
          end

          if entry.is_a?(ParallelGroup)
            results = execute_parallel_group(entry.parallel, context)
            results.each do |result|
              step_results << result
              context["steps"][result.id] = { "output" => result.output }
              accumulate_metrics!(total_metrics, result)

              # Check short-circuit on each parallel result
              step_def = entry.parallel.find { |s| s.id == result.id }
              if step_def&.short_circuit && result.status == "success"
                if evaluate_short_circuit(step_def.short_circuit[:condition], result.output)
                  short_circuited = true
                  short_circuit_defaults = step_def.short_circuit[:defaults]
                  @log.info("short_circuit:triggered", { step: result.id })
                end
              end
            end
          elsif entry.is_a?(RouteEntry)
            route_result = execute_route(entry.route, context)
            context["steps"][entry.route.id] = { "output" => route_result.output }
            step_results << route_result
            accumulate_metrics!(total_metrics, route_result)

            if route_result.status == "short_circuited"
              short_circuited = true
              none_target = entry.route.routes["_none"]
              short_circuit_defaults = none_target.is_a?(Hash) ? (none_target["defaults"] || {}) : {}
              @log.info("short_circuit:triggered", { route: entry.route.id })
            end
          else
            result = execute_step_with_retry(entry, context)
            step_results << result
            context["steps"][entry.id] = { "output" => result.output }
            accumulate_metrics!(total_metrics, result)

            # Check short-circuit
            if entry.short_circuit && result.status == "success"
              if evaluate_short_circuit(entry.short_circuit[:condition], result.output)
                short_circuited = true
                short_circuit_defaults = entry.short_circuit[:defaults]
                @log.info("short_circuit:triggered", { step: entry.id })
              end
            end
          end
        end

        # Resolve final output bindings
        final_output = Resolver.resolve_outputs(workflow_def.output, context)
        completed_at = Time.now.utc.iso8601

        status = if short_circuited
                   "short_circuited"
                 elsif step_results.any? { |r| r.status == "error" }
                   "error"
                 else
                   "success"
                 end

        envelope = WorkflowEnvelope.new(
          workflow:   workflow_def.name,
          version:    workflow_def.version,
          request_id: request_id,
          status:     status,
          timestamps: { started_at: started_at, completed_at: completed_at },
          metrics:    total_metrics,
          steps:      step_results,
          result:     final_output,
          error:      step_results.select { |r| r.status == "error" }.map(&:error).compact.first
        )

        @log.info("orchestrate:complete", { workflow: workflow_name, status: status, request_id: request_id })

        envelope.to_h
      end

      private

      # Execute a group of steps in parallel using concurrent-ruby Promises.
      #
      # @param steps [Array<WorkflowStep>]
      # @param context [Hash]
      # @return [Array<StepResult>]
      def execute_parallel_group(steps, context)
        @log.debug("parallel_group", { step_ids: steps.map(&:id) })

        # Snapshot the context so parallel steps don't see each other's writes
        frozen_context = deep_dup(context)

        futures = steps.map do |step|
          Concurrent::Promises.future do
            execute_step_with_retry(step, frozen_context)
          end
        end

        # Wait for all to complete
        combined = Concurrent::Promises.zip(*futures).value!
        # zip returns a single value if only one future; normalize to array
        Array(combined)
      end

      # Execute a single step with retry and fallback logic.
      #
      # @param step [WorkflowStep]
      # @param context [Hash]
      # @return [StepResult]
      def execute_step_with_retry(step, context)
        agent_def = Loader.load_agent(step.agent)
        resolved_input = Resolver.resolve_inputs(step.input, context)

        max_attempts = step.retry_config ? step.retry_config[:max_attempts] : 1
        backoff_ms = step.retry_config ? step.retry_config[:backoff_ms] : 0

        last_error = nil
        attempts = 0

        max_attempts.times do |attempt_index|
          attempts = attempt_index + 1
          begin
            @log.debug("step:attempt", { step: step.id, attempt: attempts, max: max_attempts })
            result = Runner.execute_agent(resolved_input, agent_def, config_overrides: step.config)

            return StepResult.new(
              id:            step.id,
              agent:         step.agent,
              status:        "success",
              output:        result[:output],
              metrics:       result[:metrics],
              attempts:      attempts,
              used_fallback: false
            )
          rescue StandardError => e
            last_error = e
            @log.warn("step:error", { step: step.id, attempt: attempts, error: e.message })
            if attempt_index < max_attempts - 1
              sleep_seconds = (backoff_ms * (attempt_index + 1)) / 1000.0
              sleep(sleep_seconds) if sleep_seconds > 0
            end
          end
        end

        # All retries exhausted -- try fallback
        if step.fallback
          @log.info("step:fallback", { step: step.id, fallback_agent: step.fallback[:agent] })
          begin
            fallback_def = Loader.load_agent(step.fallback[:agent])
            result = Runner.execute_agent(resolved_input, fallback_def, config_overrides: step.fallback[:config])

            return StepResult.new(
              id:            step.id,
              agent:         step.fallback[:agent],
              status:        "success",
              output:        result[:output],
              metrics:       result[:metrics],
              attempts:      attempts + 1,
              used_fallback: true
            )
          rescue StandardError => e
            @log.error("step:fallback_failed", { step: step.id, error: e.message })
            last_error = e
          end
        end

        # Complete failure
        StepResult.new(
          id:            step.id,
          agent:         step.agent,
          status:        "error",
          output:        nil,
          metrics:       StepMetrics.new(latency_ms: 0, input_tokens: 0, output_tokens: 0),
          attempts:      attempts,
          used_fallback: step.fallback ? true : false,
          error:         last_error&.message || "Unknown error"
        )
      end

      # Fill a skipped route block result when the workflow is short-circuited.
      def fill_route_skipped(route_block, defaults, context, step_results)
        default_output = defaults&.dig(route_block.id) || nil
        @log.info("route:skipped", { route: route_block.id })
        context["steps"][route_block.id] = { "output" => default_output }
        step_results << StepResult.new(
          id:      route_block.id,
          agent:   "router:#{route_block.router}",
          status:  "skipped",
          output:  default_output,
          metrics: StepMetrics.new(latency_ms: 0, input_tokens: 0, output_tokens: 0)
        )
      end

      # Execute a route block: decide → handle _none → dispatch target.
      #
      # @param route_block [RouteBlock]
      # @param context [Hash]
      # @return [StepResult]
      def execute_route(route_block, context)
        max_attempts = route_block.retry ? route_block.retry[:max_attempts] : 1
        backoff_ms   = route_block.retry ? route_block.retry[:backoff_ms] : 0

        @log.info("route:start", {
          route: route_block.id,
          router: route_block.router,
          route_keys: route_block.routes.keys,
          max_attempts: max_attempts,
          has_fallback: !!route_block.fallback
        })

        router_def     = Loader.load_router(route_block.router)
        resolved_input = Resolver.resolve_inputs(route_block.input, context)
        route_keys     = route_block.routes.keys.reject { |k| k == "_none" }

        # ── Phase 1: Router decision with retry (+ optional fallback) ──

        chosen_key    = nil
        router_output = nil
        used_fallback = false
        total_attempts = 0
        decided       = false
        last_error    = nil

        max_attempts.times do |attempt_index|
          total_attempts = attempt_index + 1
          @log.info("route:decision_attempt", { route: route_block.id, attempt: total_attempts, max: max_attempts })

          begin
            output = execute_router_decision(router_def, resolved_input, route_keys)
            key = output.is_a?(Hash) ? (output["route"] || output[:route]) : nil
            key = key.to_s

            @log.info("route:decision", { route: route_block.id, chosen: key, attempt: total_attempts })

            if key != "_none" && !route_block.routes.key?(key)
              raise OrchestrationError,
                    "Router \"#{route_block.router}\" returned invalid route key \"#{key}\". " \
                    "Valid keys: #{(route_keys + ["_none"]).join(", ")}"
            end

            chosen_key    = key
            router_output = output
            decided       = true
            break
          rescue StandardError => e
            last_error = e
            @log.warn("route:decision_error", { route: route_block.id, attempt: total_attempts, error: e.message })
            if attempt_index < max_attempts - 1
              sleep_seconds = (backoff_ms * (attempt_index + 1)) / 1000.0
              sleep(sleep_seconds) if sleep_seconds > 0
            end
          end
        end

        # Fallback router decision
        if !decided && route_block.fallback
          @log.info("route:fallback_decision", { route: route_block.id, fallback_router: route_block.fallback[:router] })
          begin
            fallback_def = Loader.load_router(route_block.fallback[:router])
            merged_fallback = route_block.fallback[:config] ? fallback_def.dup.tap { |d|
              route_block.fallback[:config].each { |k, v| d[k.to_sym] = v rescue nil }
            } : fallback_def
            output = execute_router_decision(merged_fallback, resolved_input, route_keys)
            key = output.is_a?(Hash) ? (output["route"] || output[:route]) : nil
            key = key.to_s

            if key != "_none" && !route_block.routes.key?(key)
              raise OrchestrationError, "Fallback router returned invalid key \"#{key}\""
            end

            chosen_key     = key
            router_output  = output
            used_fallback  = true
            total_attempts += 1
            decided        = true
          rescue StandardError => e
            last_error     = e
            total_attempts += 1
            @log.error("route:fallback_decision_error", { route: route_block.id, error: e.message })
          end
        end

        unless decided
          @log.error("route:decision_exhausted", { route: route_block.id, total_attempts: total_attempts })
          return StepResult.new(
            id:            route_block.id,
            agent:         "router:#{route_block.router}",
            status:        "error",
            output:        nil,
            metrics:       StepMetrics.new(latency_ms: 0, input_tokens: 0, output_tokens: 0),
            attempts:      total_attempts,
            used_fallback: !!route_block.fallback,
            error:         last_error&.message || "Router decision failed"
          )
        end

        resolved_agent = used_fallback ? "router:#{route_block.fallback[:router]}" : "router:#{route_block.router}"
        target = route_block.routes[chosen_key]

        # ── Phase 2: Handle _none → short_circuit ──

        if chosen_key == "_none" || (target.is_a?(Hash) && target["short_circuit"])
          route_out = RouteOutput.new(route: "_none", router_output: router_output, result: nil)
          return StepResult.new(
            id:            route_block.id,
            agent:         resolved_agent,
            status:        "short_circuited",
            output:        route_out.to_h,
            metrics:       StepMetrics.new(latency_ms: 0, input_tokens: 0, output_tokens: 0),
            attempts:      total_attempts,
            used_fallback: used_fallback
          )
        end

        # ── Phase 3: Dispatch target (no retry) ──

        dispatch_result = dispatch_route_target(target, resolved_input, route_block, context)
        route_out = RouteOutput.new(route: chosen_key, router_output: router_output, result: dispatch_result[:output])

        StepResult.new(
          id:            route_block.id,
          agent:         resolved_agent,
          status:        "success",
          output:        route_out.to_h,
          metrics:       dispatch_result[:metrics],
          attempts:      total_attempts,
          used_fallback: used_fallback
        )
      end

      # Execute the router decision, returning a hash with a "route" key.
      #
      # @param router_def [RouterDefinition]
      # @param resolved_input [Hash]
      # @param route_keys [Array<String>]
      # @return [Hash] e.g. { "route" => "sports" }
      def execute_router_decision(router_def, resolved_input, route_keys)
        if router_def.strategy == "deterministic"
          agent_compat = AgentDefinition.new(
            name:        router_def.name,
            description: router_def.description,
            type:        "deterministic",
            handler:     router_def.handler
          )
          result = Runner.execute_agent(resolved_input, agent_compat)
          result[:output]
        else
          # LLM strategy
          input_summary = resolved_input.map do |k, v|
            "#{k}: #{v.is_a?(String) ? v : JSON.generate(v)}"
          end.join("\n")

          user_message =
            "#{input_summary}\n\n" \
            "You must choose exactly one of the following routes: #{route_keys.join(", ")}\n" \
            "If none of the routes apply, choose: _none\n" \
            "Respond with a JSON object: { \"route\": \"<chosen_key>\" }"

          result = LLM.call_llm(
            model:         router_def.model || "gpt-4.1-mini",
            system_prompt: router_def.prompt || "",
            user_content:  user_message,
            temperature:   router_def.temperature || 0,
            schema_name:   nil,
            provider:      router_def.provider
          )
          result[:output]
        end
      end

      # Dispatch to the chosen route target.
      #
      # @param target [String, Hash] the route target value from the routes map
      # @param pass_through [Hash] resolved input from the route block
      # @param route_block [RouteBlock]
      # @param context [Hash]
      # @return [Hash] { output:, metrics: }
      def dispatch_route_target(target, pass_through, route_block, context)
        if target.is_a?(String)
          # String → agent ID with pass-through input
          agent_def = Loader.load_agent(target)
          Runner.execute_agent(pass_through, agent_def)

        elsif target.is_a?(Hash) && target.key?("route")
          # Nested route block
          nested_block = Loader.build_route_block(target["route"])
          nested_result = execute_route(nested_block, context)
          {
            output:  nested_result.output,
            metrics: nested_result.metrics
          }

        elsif target.is_a?(Hash) && target.key?("agent")
          # Agent with explicit or pass-through input
          agent_def  = Loader.load_agent(target["agent"])
          agent_input = target["input"] ? Resolver.resolve_inputs(target["input"], context) : pass_through
          Runner.execute_agent(agent_input, agent_def)

        elsif target.is_a?(Hash) && target.key?("workflow")
          # Workflow target
          wf_input = target["input"] ? Resolver.resolve_inputs(target["input"], context) : pass_through
          envelope = orchestrate(target["workflow"], wf_input)
          {
            output:  envelope[:result],
            metrics: {
              latency_ms:    envelope.dig(:metrics, :total_latency_ms) || 0,
              input_tokens:  envelope.dig(:metrics, :total_input_tokens) || 0,
              output_tokens: envelope.dig(:metrics, :total_output_tokens) || 0
            }
          }

        else
          raise OrchestrationError, "Unknown route target type in route #{route_block.id}: #{target.inspect}"
        end
      end

      # Evaluate a short-circuit condition string.
      # The condition is a Ruby expression with +output+ in scope.
      #
      # @param condition [String] e.g. "!output['found']" or "!output.is_food"
      # @param output [Object] the step output
      # @return [Boolean]
      def evaluate_short_circuit(condition, output)
        # Normalize JS-style property access to Ruby hash access:
        #   !output.found  =>  !output["found"]
        #   output.status == "fail"  =>  output["status"] == "fail"
        normalized = condition.gsub(/output\.(\w+)/) do
          "output[\"#{::Regexp.last_match(1)}\"]"
        end

        # rubocop:disable Security/Eval
        binding_context = binding
        binding_context.local_variable_set(:output, output)
        result = eval(normalized, binding_context) # rubocop:disable Lint/Eval
        # rubocop:enable Security/Eval

        @log.debug("short_circuit:eval", { condition: condition, normalized: normalized, result: result })
        !!result
      rescue StandardError => e
        @log.warn("short_circuit:eval_error", { condition: condition, error: e.message })
        false
      end

      def accumulate_metrics!(total, step_result)
        m = step_result.metrics
        if m.is_a?(StepMetrics)
          total[:total_latency_ms] += m.latency_ms
          total[:total_input_tokens] += m.input_tokens
          total[:total_output_tokens] += m.output_tokens
        elsif m.is_a?(Hash)
          total[:total_latency_ms] += (m[:latency_ms] || m["latency_ms"] || 0)
          total[:total_input_tokens] += (m[:input_tokens] || m["input_tokens"] || 0)
          total[:total_output_tokens] += (m[:output_tokens] || m["output_tokens"] || 0)
        end

        if step_result.status == "skipped"
          total[:steps_skipped] += 1
        else
          total[:steps_executed] += 1
        end
      end

      # Deep-duplicate a hash/array structure (cheap snapshot for parallel).
      def deep_dup(obj)
        case obj
        when Hash
          obj.each_with_object({}) { |(k, v), h| h[k] = deep_dup(v) }
        when Array
          obj.map { |v| deep_dup(v) }
        else
          obj
        end
      end

      # Recursively convert symbol keys to string keys.
      def stringify_keys(obj)
        case obj
        when Hash
          obj.each_with_object({}) { |(k, v), h| h[k.to_s] = stringify_keys(v) }
        when Array
          obj.map { |v| stringify_keys(v) }
        else
          obj
        end
      end
    end
  end
end
