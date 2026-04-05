# frozen_string_literal: true

require "securerandom"
require "concurrent"

module AgenticEngine
  module Orchestrator
    class OrchestrationError < StandardError; end

    ZERO_METRICS = StepMetrics.new(latency_ms: 0, input_tokens: 0, output_tokens: 0).freeze

    @log = Logger.create("orchestrator")

    class << self
      def orchestrate(workflow_name, input)
        started_at = Time.now.utc.iso8601
        request_id = SecureRandom.uuid
        workflow_def = Loader.load_workflow(workflow_name)

        @log.info("orchestrate:start", { workflow: workflow_name, request_id: request_id })

        context = { "input" => stringify_keys(input), "steps" => {} }
        step_results = []
        trail = []
        status = "success"
        error_msg = nil

        # Build step index for graph traversal
        step_index = {}
        workflow_def.steps.each_with_index do |entry, i|
          sid = get_step_id(entry)
          step_index[sid] = i if sid
          # Also index parallel branch IDs
          if entry.is_a?(ParallelBlock)
            entry.branches.each { |b| step_index[b.id] = i if b.id }
          end
        end

        executed_steps = Set.new

        begin
          cursor = 0
          while cursor < workflow_def.steps.length
            entry = workflow_def.steps[cursor]
            sid = get_step_id(entry)
            executed_steps.add(sid)

            next_target = case entry
                          when WorkflowStep
                            execute_workflow_step(entry, context, step_results, trail)
                          when ParallelBlock
                            execute_parallel_block(entry, context, step_results, trail)
                          when LoopBlock
                            execute_loop_block(entry, context, step_results, trail)
                          when ForEachBlock
                            execute_for_each_block(entry, context, step_results, trail)
                          else
                            ""
                          end

            if next_target == "_end"
              break
            elsif next_target && !next_target.empty?
              idx = step_index[next_target]
              raise OrchestrationError, "next target '#{next_target}' not found" unless idx
              cursor = idx
            else
              cursor += 1
            end
          end
        rescue StandardError => e
          status = "error"
          error_msg = e.message
          @log.error("orchestrate:error", { workflow: workflow_name, error: e.message })
        end

        # Mark non-executed steps
        workflow_def.steps.each do |entry|
          sid = get_step_id(entry)
          mark_not_executed(entry, step_results) unless executed_steps.include?(sid)
        end

        # Resolve outputs
        final_output = begin
          Resolver.resolve_outputs(workflow_def.output, context)
        rescue StandardError
          {}
        end

        completed_at = Time.now.utc.iso8601
        total_metrics = compute_total_metrics(step_results)

        envelope = WorkflowEnvelope.new(
          workflow:   workflow_def.name,
          version:    workflow_def.version,
          request_id: request_id,
          status:     status,
          timestamps: { started_at: started_at, completed_at: completed_at },
          metrics:    total_metrics,
          steps:      step_results,
          trail:      trail,
          result:     final_output,
          error:      error_msg
        )

        @log.info("orchestrate:complete", { workflow: workflow_name, status: status })

        if status == "error"
          raise WorkflowError.new(error_msg || "unknown error", envelope: envelope.to_h)
        end

        envelope.to_h
      end

      private

      # ── Step execution ──

      def execute_workflow_step(step, context, results, trail)
        emit_trail(trail, step.id, "step_start")

        sr = if step.workflow && !step.workflow.empty?
               execute_sub_workflow(step, context)
             else
               execute_agent_step(step.id, step.agent, step.input, step.config,
                                  step.retry_config, step.fallback, context)
             end

        results << sr

        if sr.status == "success"
          context["steps"][step.id] = { "output" => sr.output }
          emit_trail(trail, step.id, "step_success")
        else
          emit_trail(trail, step.id, "step_error", { error: sr.error })
          raise OrchestrationError, "Step '#{step.id}' failed: #{sr.error}"
        end

        resolve_next_target(step.next_field, sr.output)
      end

      def execute_sub_workflow(step, context)
        resolved_input = Resolver.resolve_inputs(step.input, context)
        start_time = Process.clock_gettime(Process::CLOCK_MONOTONIC)

        begin
          sub_envelope = orchestrate(step.workflow, resolved_input)
          elapsed_ms = ((Process.clock_gettime(Process::CLOCK_MONOTONIC) - start_time) * 1000).round(2)

          StepResult.new(
            id:           step.id,
            workflow:     step.workflow,
            status:       "success",
            output:       sub_envelope[:result],
            sub_envelope: sub_envelope,
            metrics:      StepMetrics.new(
              latency_ms:    elapsed_ms,
              input_tokens:  sub_envelope.dig(:metrics, :total_input_tokens) || 0,
              output_tokens: sub_envelope.dig(:metrics, :total_output_tokens) || 0
            )
          )
        rescue WorkflowError => e
          elapsed_ms = ((Process.clock_gettime(Process::CLOCK_MONOTONIC) - start_time) * 1000).round(2)
          StepResult.new(
            id:           step.id,
            workflow:     step.workflow,
            status:       "error",
            sub_envelope: e.envelope,
            metrics:      StepMetrics.new(latency_ms: elapsed_ms, input_tokens: 0, output_tokens: 0),
            error:        e.message
          )
        end
      end

      def execute_agent_step(step_id, agent_id, input_bindings, config, retry_config, fallback, context)
        resolved_input = Resolver.resolve_inputs(input_bindings, context)
        config_overrides = config || {}

        max_attempts = retry_config ? retry_config.max_attempts : 1
        backoff_ms = retry_config ? retry_config.backoff_ms : 0
        last_error = nil

        max_attempts.times do |attempt_index|
          attempt = attempt_index + 1
          begin
            agent_def = Loader.load_agent(agent_id)
            result = Runner.execute_agent(resolved_input, agent_def, config_overrides: config_overrides)

            return StepResult.new(
              id:       step_id,
              agent:    agent_id,
              status:   "success",
              output:   result[:output],
              metrics:  result[:metrics],
              attempts: attempt
            )
          rescue StandardError => e
            last_error = e
            @log.warn("step:attempt_failed", { step: step_id, attempt: attempt, error: e.message })
            if attempt_index < max_attempts - 1
              sleep((backoff_ms * (attempt_index + 1)) / 1000.0)
            end
          end
        end

        # Fallback
        if fallback && fallback.agent && !fallback.agent.empty?
          @log.info("step:fallback", { step: step_id, fallback_agent: fallback.agent })
          begin
            fallback_def = Loader.load_agent(fallback.agent)
            fb_overrides = config_overrides.merge(fallback.config || {})
            result = Runner.execute_agent(resolved_input, fallback_def, config_overrides: fb_overrides)

            return StepResult.new(
              id:              step_id,
              agent:           fallback.agent,
              status:          "success",
              output:          result[:output],
              metrics:         result[:metrics],
              attempts:        max_attempts,
              used_fallback:   true,
              fallback_reason: last_error&.message
            )
          rescue StandardError => e
            last_error = e
          end
        end

        StepResult.new(
          id:       step_id,
          agent:    agent_id,
          status:   "error",
          output:   nil,
          metrics:  ZERO_METRICS,
          attempts: max_attempts,
          error:    last_error&.message || "Unknown error"
        )
      end

      # ── Parallel execution ──

      def execute_parallel_block(pb, context, results, trail)
        emit_trail(trail, pb.id, "parallel_start", { join: pb.join, branches: pb.branches.length })

        frozen_context = deep_dup(context)

        futures = pb.branches.map do |branch|
          Concurrent::Promises.future do
            if branch.workflow && !branch.workflow.empty?
              ws = WorkflowStep.new(id: branch.id, workflow: branch.workflow, input: branch.input, config: branch.config)
              execute_sub_workflow(ws, frozen_context)
            else
              execute_agent_step(branch.id, branch.agent, branch.input, branch.config,
                                 branch.retry_config, branch.fallback, frozen_context)
            end
          end
        end

        branch_results = Concurrent::Promises.zip(*futures).value!
        branch_results = Array(branch_results)

        has_error = false
        branch_results.each do |sr|
          results << sr
          if sr.status == "success"
            context["steps"][sr.id] = { "output" => sr.output }
          else
            has_error = true
          end
        end

        emit_trail(trail, pb.id, "parallel_end")

        case pb.join
        when "all"
          raise OrchestrationError, "Parallel block '#{pb.id}': one or more branches failed (join=all)" if has_error
        when "any"
          unless branch_results.any? { |r| r.status == "success" }
            raise OrchestrationError, "Parallel block '#{pb.id}': all branches failed (join=any)"
          end
        end
        # all_settled: always continue

        resolve_next_target(pb.next_field, nil)
      end

      # ── Loop execution ──

      def execute_loop_block(lb, context, results, trail)
        last_result = nil

        lb.max_iterations.times do |i|
          iteration = i + 1
          emit_trail(trail, lb.id, "loop_iteration", { iteration: iteration })

          sr = if lb.workflow && !lb.workflow.empty?
                 ws = WorkflowStep.new(id: lb.id, workflow: lb.workflow, input: lb.input, config: lb.config)
                 execute_sub_workflow(ws, context)
               else
                 execute_agent_step(lb.id, lb.agent, lb.input, lb.config,
                                    lb.retry_config, lb.fallback, context)
               end

          if sr.status == "success"
            context["steps"][lb.id] = { "output" => sr.output }
          else
            results << sr
            raise OrchestrationError, "Loop '#{lb.id}' iteration #{iteration} failed: #{sr.error}"
          end

          last_result = sr

          if lb.until_condition && !lb.until_condition.empty?
            break if evaluate_condition(lb.until_condition, sr.output)
          end
        end

        results << last_result if last_result

        resolve_next_target(lb.next_field, nil)
      end

      # ── ForEach execution ──

      def execute_for_each_block(feb, context, results, trail)
        collection = Resolver.resolve_ref(feb.collection, context)
        raise OrchestrationError, "for_each '#{feb.id}': collection did not resolve to an array" unless collection.is_a?(Array)

        if collection.empty?
          context["steps"][feb.id] = { "output" => [] }
          results << StepResult.new(id: feb.id, agent: feb.agent, status: "success", output: [], metrics: ZERO_METRICS)
          return resolve_next_target(feb.next_field, nil)
        end

        concurrency = feb.max_concurrency.to_i > 0 ? feb.max_concurrency : collection.length
        iter_results = Array.new(collection.length)
        iter_errors = Array.new(collection.length)

        # Process in batches for concurrency control
        collection.each_slice(concurrency).with_index do |batch, batch_idx|
          futures = batch.each_with_index.map do |item, local_idx|
            idx = batch_idx * concurrency + local_idx
            Concurrent::Promises.future do
              emit_trail(trail, feb.id, "for_each_iteration", { index: idx })
              iter_ctx = deep_dup(context)
              iter_ctx["steps"]["__current"] = { "output" => item }

              sr = execute_agent_step(
                "#{feb.id}[#{idx}]", feb.agent, feb.input, feb.config,
                feb.retry_config, feb.fallback, iter_ctx
              )

              if sr.status == "success"
                iter_results[idx] = sr.output
              else
                iter_errors[idx] = sr.error || "unknown"
              end
            end
          end
          Concurrent::Promises.zip(*futures).value!
        end

        has_error = iter_errors.any? { |e| !e.nil? }
        has_success = iter_errors.any?(&:nil?)

        fe_status = if has_error && has_success
                      "partial_failure"
                    elsif has_error
                      "error"
                    else
                      "success"
                    end

        results << StepResult.new(id: feb.id, agent: feb.agent, status: fe_status, output: iter_results, metrics: ZERO_METRICS)
        context["steps"][feb.id] = { "output" => iter_results }

        if fe_status == "error"
          first_err = iter_errors.compact.first
          raise OrchestrationError, "for_each '#{feb.id}': #{first_err}"
        end

        resolve_next_target(feb.next_field, nil)
      end

      # ── Next resolution ──

      def resolve_next_target(next_field, output)
        return "" if next_field.nil?

        if next_field.target && !next_field.target.empty?
          return next_field.target
        end

        if next_field.switch
          sn = next_field.switch
          output_map = output.is_a?(Hash) ? output : {}
          val = resolve_field_path(sn.expression, output_map)
          val_str = val.nil? ? "" : val.to_s
          return sn.cases[val_str] || sn.default || ""
        end

        if next_field.if_next
          ifn = next_field.if_next
          return evaluate_condition(ifn.condition, output) ? (ifn.then_target || "") : (ifn.else_target || "")
        end

        ""
      end

      # ── Condition evaluation ──

      def evaluate_condition(condition, output)
        output_map = output.is_a?(Hash) ? output : {}
        condition = condition.strip

        if condition.start_with?("!")
          return !evaluate_positive(condition[1..].strip, output_map)
        end

        if condition.include?("==")
          left, right = condition.split("==", 2).map(&:strip)
          left_val = resolve_field_path(left, output_map)
          right_val = right.gsub(/^["']|["']$/, "")
          return left_val.to_s == right_val
        end

        if condition.include?(">=")
          left, right = condition.split(">=", 2).map(&:strip)
          left_val = resolve_field_path(left, output_map)
          return to_float(left_val) >= to_float(right)
        end

        if condition.include?(">")
          left, right = condition.split(">", 2).map(&:strip)
          left_val = resolve_field_path(left, output_map)
          return to_float(left_val) > to_float(right)
        end

        evaluate_positive(condition, output_map)
      end

      def evaluate_positive(expr, output_map)
        val = resolve_field_path(expr, output_map)
        truthy?(val)
      end

      def resolve_field_path(path, output_map)
        path = path.sub(/^output\./, "")
        current = output_map
        path.split(".").each do |part|
          return nil unless current.is_a?(Hash)
          current = current[part] || current[part.to_sym]
        end
        current
      end

      def truthy?(val)
        return false if val.nil?
        return val if val.is_a?(TrueClass) || val.is_a?(FalseClass)
        return val != "" if val.is_a?(String)
        return val != 0 if val.is_a?(Numeric)
        true
      end

      def to_float(v)
        Float(v)
      rescue ArgumentError, TypeError
        0.0
      end

      # ── Helpers ──

      def get_step_id(entry)
        entry.respond_to?(:id) ? entry.id : nil
      end

      def emit_trail(trail, step_id, event, data = nil)
        trail << TrailEntry.new(step_id: step_id, event: event, timestamp: Time.now.utc.iso8601, data: data)
      end

      def mark_not_executed(entry, results)
        if entry.is_a?(ParallelBlock)
          entry.branches.each do |b|
            results << StepResult.new(id: b.id, agent: b.agent, status: "not_executed", output: nil, metrics: ZERO_METRICS)
          end
        else
          results << StepResult.new(id: get_step_id(entry), agent: entry.respond_to?(:agent) ? entry.agent : nil,
                                     status: "not_executed", output: nil, metrics: ZERO_METRICS)
        end
      end

      def compute_total_metrics(step_results)
        total = { total_latency_ms: 0, total_input_tokens: 0, total_output_tokens: 0 }
        step_results.each do |sr|
          m = sr.metrics
          if m.is_a?(StepMetrics)
            total[:total_latency_ms] += m.latency_ms
            total[:total_input_tokens] += m.input_tokens
            total[:total_output_tokens] += m.output_tokens
          elsif m.is_a?(Hash)
            total[:total_latency_ms] += (m[:latency_ms] || 0)
            total[:total_input_tokens] += (m[:input_tokens] || 0)
            total[:total_output_tokens] += (m[:output_tokens] || 0)
          end
        end
        total
      end

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
