# frozen_string_literal: true

require "yaml"
require "json"

module AgenticEngine
  module Loader
    @agents    = {}
    @workflows = {}
    @schemas   = {}
    @root      = nil

    class << self
      attr_reader :agents, :workflows, :schemas

      def reset!
        @agents.clear
        @workflows.clear
        @schemas.clear
        @root = nil
      end

      # ── Project root discovery ──

      def find_root(start = Dir.pwd)
        dir = File.expand_path(start)
        loop do
          return dir if File.exist?(File.join(dir, "agentic.config.yaml"))
          return dir if File.directory?(File.join(dir, "agentic-spec"))
          return dir if File.directory?(File.join(dir, "agents"))
          parent = File.dirname(dir)
          return nil if parent == dir
          dir = parent
        end
      end

      def root
        @root ||= find_root or raise "Cannot locate agentic project root"
      end

      # ── Agent loading ──

      def load_agent(agent_id)
        return @agents[agent_id] if @agents.key?(agent_id)

        agent_dir = File.join(root, "agentic-spec", "agents", agent_id)
        yaml_path = File.join(agent_dir, "agent.yaml")
        raise "Agent not found: #{agent_id} (looked in #{yaml_path})" unless File.exist?(yaml_path)

        raw = YAML.safe_load(File.read(yaml_path), permitted_classes: [Symbol])

        prompt = nil
        prompt_path = File.join(agent_dir, "prompt.md")
        prompt = File.read(prompt_path) if File.exist?(prompt_path)

        definition = AgentDefinition.new(
          name:         raw["name"],
          description:  raw["description"],
          type:         raw["type"],
          base_url:     raw["base_url"],
          api_key_env:  raw["api_key_env"],
          model:        raw["model"],
          temperature:  raw["temperature"],
          input_type:   raw["input_type"] || "text",
          image_detail: raw["image_detail"] || "auto",
          schema:       raw["schema"],
          user_message: raw["user_message"],
          handler:      raw["handler"],
          prompt:       prompt,
          input:        raw["input"] || {}
        )

        @agents[agent_id] = definition
      end

      # ── Workflow loading ──

      def load_workflow(workflow_name)
        return @workflows[workflow_name] if @workflows.key?(workflow_name)

        yaml_path = File.join(root, "agentic-spec", "workflows", "#{workflow_name}.yaml")
        raise "Workflow not found: #{workflow_name} (looked in #{yaml_path})" unless File.exist?(yaml_path)

        raw = YAML.safe_load(File.read(yaml_path), permitted_classes: [Symbol])

        steps = (raw["steps"] || []).map { |entry| parse_step_entry(entry) }

        definition = WorkflowDefinition.new(
          name:        raw["name"],
          description: raw["description"],
          version:     raw["version"],
          input:       raw["input"] || {},
          steps:       steps,
          output:      raw["output"] || {}
        )

        @workflows[workflow_name] = definition
      end

      # ── Schema loading ──

      def load_schema(schema_name)
        return @schemas[schema_name] if @schemas.key?(schema_name)

        json_path = File.join(root, "agentic-spec", "schemas", "#{schema_name}.json")
        raise "Schema not found: #{schema_name} (looked in #{json_path})" unless File.exist?(json_path)

        parsed = JSON.parse(File.read(json_path))
        @schemas[schema_name] = parsed
      end

      private

      def parse_step_entry(entry)
        if entry.key?("parallel")
          parse_parallel_block(entry["parallel"])
        elsif entry.key?("loop")
          parse_loop_block(entry["loop"])
        elsif entry.key?("for_each")
          parse_for_each_block(entry["for_each"])
        else
          parse_workflow_step(entry)
        end
      end

      def parse_workflow_step(raw)
        WorkflowStep.new(
          id:           raw["id"],
          agent:        raw["agent"],
          workflow:     raw["workflow"],
          input:        raw["input"] || {},
          config:       raw["config"],
          retry_config: parse_retry(raw["retry"]),
          fallback:     parse_fallback(raw["fallback"]),
          requires:     raw["requires"] || [],
          next_field:   parse_next(raw["next"])
        )
      end

      def parse_parallel_block(raw)
        branches = (raw["branches"] || []).map do |b|
          ParallelBranch.new(
            id:           b["id"],
            agent:        b["agent"],
            workflow:     b["workflow"],
            input:        b["input"] || {},
            config:       b["config"],
            retry_config: parse_retry(b["retry"]),
            fallback:     parse_fallback(b["fallback"])
          )
        end

        ParallelBlock.new(
          id:         raw["id"],
          join:       raw["join"] || "all",
          branches:   branches,
          next_field: parse_next(raw["next"])
        )
      end

      def parse_loop_block(raw)
        LoopBlock.new(
          id:              raw["id"],
          agent:           raw["agent"],
          workflow:        raw["workflow"],
          input:           raw["input"] || {},
          config:          raw["config"],
          until_condition: raw["until"],
          max_iterations:  raw["max_iterations"] || 1,
          retry_config:    parse_retry(raw["retry"]),
          fallback:        parse_fallback(raw["fallback"]),
          next_field:      parse_next(raw["next"])
        )
      end

      def parse_for_each_block(raw)
        ForEachBlock.new(
          id:              raw["id"],
          agent:           raw["agent"],
          workflow:        raw["workflow"],
          input:           raw["input"] || {},
          config:          raw["config"],
          collection:      raw["collection"],
          max_concurrency: raw["max_concurrency"] || 0,
          retry_config:    parse_retry(raw["retry"]),
          fallback:        parse_fallback(raw["fallback"]),
          next_field:      parse_next(raw["next"])
        )
      end

      def parse_retry(raw)
        return nil unless raw
        RetryConfig.new(
          max_attempts: raw["max_attempts"] || 1,
          backoff_ms:   raw["backoff_ms"] || 0
        )
      end

      def parse_fallback(raw)
        return nil unless raw
        FallbackConfig.new(
          agent:    raw["agent"],
          workflow: raw["workflow"],
          config:   raw["config"] || {}
        )
      end

      def parse_next(raw)
        return nil if raw.nil?

        # Simple string target
        if raw.is_a?(String)
          return NextField.new(target: raw)
        end

        if raw.is_a?(Hash)
          if raw.key?("switch")
            return NextField.new(switch: SwitchNext.new(
              expression: raw["switch"],
              cases:      raw["cases"] || {},
              default:    raw["default"]
            ))
          end

          if raw.key?("if")
            return NextField.new(if_next: IfNext.new(
              condition:   raw["if"],
              then_target: raw["then"],
              else_target: raw["else"]
            ))
          end

          raise "next: mapping must contain 'switch' or 'if' key, got: #{raw.keys.inspect}"
        end

        raise "next: must be a string or mapping, got: #{raw.class}"
      end
    end
  end
end
