# frozen_string_literal: true

module AgenticEngine
  # Resolves $-path references and {{template}} placeholders used in workflow
  # bindings and agent user_message templates.
  #
  # Context shape:
  #   { "input" => { ... }, "steps" => { "step_id" => { "output" => ... } } }
  module Resolver
    class ResolutionError < StandardError; end

    class << self
      # Resolve a single $.path reference against a context hash.
      #
      # @param ref [String] e.g. "$.input.product_id" or "$.steps.fetch.output.title"
      # @param context [Hash]
      # @return [Object] the resolved value
      def resolve_ref(ref, context)
        return ref unless ref.is_a?(String) && ref.start_with?("$.")

        parts = ref.sub(/^\$\./, "").split(".")
        current = context

        parts.each do |key|
          case current
          when Hash
            # Try string keys first (YAML loads as strings), then symbols
            if current.key?(key)
              current = current[key]
            elsif current.key?(key.to_sym)
              current = current[key.to_sym]
            else
              raise ResolutionError, "Cannot resolve '#{ref}': key '#{key}' not found in #{current.keys.inspect}"
            end
          else
            raise ResolutionError, "Cannot resolve '#{ref}': expected Hash at '#{key}', got #{current.class}"
          end
        end

        current
      end

      # Resolve all input bindings for a workflow step.
      #
      # Each value in +bindings+ is either a $.path string to resolve or a
      # literal value to pass through.
      #
      # @param bindings [Hash] e.g. { "product_name" => "$.steps.fetch.output.title" }
      # @param context [Hash]
      # @return [Hash] resolved input hash
      def resolve_inputs(bindings, context)
        bindings.each_with_object({}) do |(key, value), resolved|
          resolved[key] = resolve_value(value, context)
        end
      end

      # Resolve output bindings for the final workflow result.
      #
      # @param bindings [Hash] e.g. { "product" => "$.steps.fetch.output" }
      # @param context [Hash]
      # @return [Hash]
      def resolve_outputs(bindings, context)
        bindings.each_with_object({}) do |(key, value), resolved|
          resolved[key] = resolve_value(value, context)
        end
      end

      # Interpolate {{key}} placeholders in a template string.
      #
      # Values are looked up from the +input+ hash.  Object/array values are
      # JSON-serialized for inline substitution.
      #
      # @param template [String]
      # @param input [Hash]
      # @return [String]
      def resolve_template(template, input)
        return template unless template.is_a?(String)

        template.gsub(/\{\{(\S+?)\}\}/) do |_match|
          key = ::Regexp.last_match(1)
          value = dig_value(input, key.split("."))
          case value
          when Hash, Array
            JSON.generate(value)
          when nil
            ""
          else
            value.to_s
          end
        end
      end

      private

      # Resolve a value that may be a $.ref string, a nested hash/array of
      # refs, or a plain literal.
      def resolve_value(value, context)
        case value
        when String
          value.start_with?("$.") ? resolve_ref(value, context) : value
        when Hash
          value.each_with_object({}) { |(k, v), h| h[k] = resolve_value(v, context) }
        when Array
          value.map { |v| resolve_value(v, context) }
        else
          value
        end
      end

      # Dig into a nested hash/struct using dot-separated key parts.
      def dig_value(obj, parts)
        current = obj
        parts.each do |key|
          case current
          when Hash
            current = current[key] || current[key.to_sym]
          else
            return nil
          end
        end
        current
      end
    end
  end
end
