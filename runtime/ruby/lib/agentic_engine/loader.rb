# frozen_string_literal: true

require "yaml"
require "json"

module AgenticEngine
  # Loads agent definitions, workflow definitions, and JSON schemas from the
  # project directory tree.  Walks upward from Dir.pwd looking for either
  # agentic.config.yaml or an agents/ directory, then caches all loaded
  # artefacts in module-level hashes.
  module Loader
    @agents    = {} # { agent_id => AgentDefinition }
    @workflows = {} # { workflow_name => WorkflowDefinition }
    @schemas   = {} # { schema_name => Hash (parsed JSON schema wrapper) }
    @routers   = {} # { router_id => RouterDefinition }
    @root      = nil

    class << self
      attr_reader :agents, :workflows, :schemas

      # Reset all caches (useful in tests).
      def reset!
        @agents.clear
        @workflows.clear
        @schemas.clear
        @routers.clear
        @root = nil
      end

      # ── Project root discovery ──

      # Walk up from +start+ looking for agentic.config.yaml or agents/ dir.
      #
      # @param start [String] directory to begin searching from (default: Dir.pwd)
      # @return [String, nil]
      def find_root(start = Dir.pwd)
        dir = File.expand_path(start)
        loop do
          return dir if File.exist?(File.join(dir, "agentic.config.yaml"))
          return dir if File.directory?(File.join(dir, "agents"))
          parent = File.dirname(dir)
          return nil if parent == dir # reached filesystem root
          dir = parent
        end
      end

      # Resolved project root (memoized).
      def root
        @root ||= find_root or raise "Cannot locate agentic project root (no agentic.config.yaml or agents/ found)"
      end

      # ── Agent loading ──

      # Load a single agent by its directory-name ID (e.g. "review-analyzer").
      #
      # @param agent_id [String]
      # @return [AgentDefinition]
      def load_agent(agent_id)
        return @agents[agent_id] if @agents.key?(agent_id)

        agent_dir = File.join(root, "agents", agent_id)
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
          provider:     raw["provider"],
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

      # ── Router loading ──

      # Load a single router by its directory-name ID (e.g. "content-classifier").
      #
      # @param router_id [String]
      # @return [RouterDefinition]
      def load_router(router_id)
        return @routers[router_id] if @routers.key?(router_id)

        router_dir = File.join(root, "routers", router_id)
        yaml_path = File.join(router_dir, "router.yaml")
        raise "Router not found: #{router_id} (looked in #{yaml_path})" unless File.exist?(yaml_path)

        raw = YAML.safe_load(File.read(yaml_path), permitted_classes: [Symbol])

        prompt = nil
        prompt_path = File.join(router_dir, "prompt.md")
        prompt = File.read(prompt_path).strip if File.exist?(prompt_path)

        definition = RouterDefinition.new(
          name:        raw["name"],
          description: raw["description"],
          strategy:    raw["strategy"],
          provider:    raw["provider"],
          model:       raw["model"],
          temperature: raw["temperature"],
          handler:     raw["handler"],
          prompt:      prompt,
          input:       raw["input"] || {}
        )

        @routers[router_id] = definition
      end

      # ── Workflow loading ──

      # Load a workflow by name (filename without .yaml).
      #
      # @param workflow_name [String]
      # @return [WorkflowDefinition]
      def load_workflow(workflow_name)
        return @workflows[workflow_name] if @workflows.key?(workflow_name)

        yaml_path = File.join(root, "workflows", "#{workflow_name}.yaml")
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

      # Load a JSON schema from schemas/<name>.json.
      #
      # @param schema_name [String]
      # @return [Hash] the full schema wrapper (with "name", "strict", "schema" keys)
      def load_schema(schema_name)
        return @schemas[schema_name] if @schemas.key?(schema_name)

        json_path = File.join(root, "schemas", "#{schema_name}.json")
        raise "Schema not found: #{schema_name} (looked in #{json_path})" unless File.exist?(json_path)

        parsed = JSON.parse(File.read(json_path))
        @schemas[schema_name] = parsed
      end

      private

      # Parse a single step entry which may be a plain step, a parallel group,
      # or a route block.
      def parse_step_entry(entry)
        if entry.key?("parallel")
          ParallelGroup.new(
            parallel: entry["parallel"].map { |s| build_step(s) }
          )
        elsif entry.key?("route")
          RouteEntry.new(route: build_route_block(entry["route"]))
        else
          build_step(entry)
        end
      end

      def build_route_block(raw)
        retry_config = if raw["retry"]
                         { max_attempts: raw["retry"]["max_attempts"], backoff_ms: raw["retry"]["backoff_ms"] }
                       end
        fallback = if raw["fallback"]
                     { router: raw["fallback"]["router"], config: raw["fallback"]["config"] }
                   end

        RouteBlock.new(
          id:       raw["id"],
          router:   raw["router"],
          input:    raw["input"] || {},
          routes:   raw["routes"] || {},
          retry:    retry_config,
          fallback: fallback
        )
      end

      def build_step(raw)
        WorkflowStep.new(
          id:            raw["id"],
          agent:         raw["agent"],
          input:         raw["input"] || {},
          config:        raw["config"],
          retry_config:  raw["retry"] ? { max_attempts: raw["retry"]["max_attempts"], backoff_ms: raw["retry"]["backoff_ms"] } : nil,
          fallback:      raw["fallback"] ? { agent: raw["fallback"]["agent"], config: raw["fallback"]["config"] } : nil,
          short_circuit: raw["short_circuit"] ? { condition: raw["short_circuit"]["condition"], defaults: raw["short_circuit"]["defaults"] } : nil
        )
      end
    end
  end
end
