# frozen_string_literal: true

module AgenticEngine
  module Resolver
    class ResolutionError < StandardError; end

    class << self
      # Resolve a single $.path reference against a context hash.
      # Supports: $.input.key, $.steps.sid.output.field, $.steps.sid.output[0],
      # $.current (sugar for $.steps.__current.output)
      def resolve_ref(ref, context)
        return ref unless ref.is_a?(String) && ref.start_with?("$.")

        tokens = tokenize_path(ref.sub(/^\$\./, ""))
        root = tokens.first
        remaining = tokens[1..]

        case root
        when "input"
          traverse(context["input"], remaining)
        when "current"
          current_data = context.dig("steps", "__current")
          return nil unless current_data
          remaining.empty? ? current_data["output"] : traverse(current_data["output"], remaining)
        when "steps"
          traverse(context["steps"], remaining)
        else
          raise ResolutionError, "Unknown reference root '#{root}' in '#{ref}'"
        end
      end

      # Resolve all input bindings for a workflow step.
      def resolve_inputs(bindings, context)
        bindings.each_with_object({}) do |(key, value), resolved|
          resolved[key] = resolve_value(value, context)
        end
      end

      # Resolve output bindings for the final workflow result.
      def resolve_outputs(bindings, context)
        bindings.each_with_object({}) do |(key, value), resolved|
          resolved[key] = resolve_value(value, context)
        end
      end

      # Interpolate {{key}} placeholders in a template string.
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

      # Tokenize a dotted path, splitting array indices.
      # "steps.fetch.output[0].name" -> ["steps", "fetch", "output", "[0]", "name"]
      def tokenize_path(path)
        tokens = []
        path.split(".").each do |part|
          next if part.empty?
          idx = part.index("[")
          if idx
            field = part[0...idx]
            tokens << field unless field.empty?
            tokens << part[idx..]  # e.g. "[0]"
          else
            tokens << part
          end
        end
        tokens
      end

      # Walk a token path through nested hashes and arrays.
      def traverse(current, tokens)
        tokens.each do |token|
          return nil if current.nil?

          if token.start_with?("[") && token.end_with?("]")
            idx = token[1..-2].to_i
            return nil unless current.is_a?(Array) && idx >= 0 && idx < current.length
            current = current[idx]
          elsif current.is_a?(Hash)
            current = current[token] || current[token.to_sym]
          else
            return nil
          end
        end
        current
      end

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
