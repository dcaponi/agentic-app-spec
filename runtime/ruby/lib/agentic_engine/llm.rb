# frozen_string_literal: true

require "openai"
require "json"

module AgenticEngine
  # Thin wrapper around the ruby-openai gem that handles both structured-output
  # (JSON Schema) mode and plain json_object mode.
  module LLM
    class LLMError < StandardError; end

    @log = Logger.create("llm")

    class << self
      # Call an LLM via the OpenAI-compatible API.
      #
      # @param model [String] e.g. "gpt-4.1"
      # @param system_prompt [String, nil] system message content
      # @param user_content [String] user message content
      # @param temperature [Float] sampling temperature (default 0.7)
      # @param schema_name [String, nil] if non-nil, load the named schema and
      #   use structured outputs (response_format with json_schema); otherwise
      #   fall back to { type: "json_object" }.
      # @return [Hash] { output: <parsed JSON>, metrics: StepMetrics }
      def call_llm(model:, system_prompt: nil, user_content:, temperature: 0.7, schema_name: nil)
        @log.debug("call_llm", { model: model, schema: schema_name, temperature: temperature })

        messages = build_messages(system_prompt: system_prompt, user_content: user_content)
        params = {
          model: model,
          messages: messages,
          temperature: temperature
        }

        if schema_name
          schema_def = Loader.load_schema(schema_name)
          params[:response_format] = {
            type: "json_schema",
            json_schema: {
              name: schema_def["name"],
              strict: schema_def.fetch("strict", true),
              schema: schema_def["schema"]
            }
          }
        else
          params[:response_format] = { type: "json_object" }
        end

        start_time = Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond)
        response = client.chat(parameters: params)
        end_time = Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond)

        choice = response.dig("choices", 0)
        raise LLMError, "No choices returned from LLM" unless choice

        finish_reason = choice["finish_reason"]
        if finish_reason == "refusal"
          refusal = choice.dig("message", "refusal")
          raise LLMError, "LLM refused request: #{refusal}"
        end

        raw_content = choice.dig("message", "content")
        raise LLMError, "Empty content from LLM" if raw_content.nil? || raw_content.strip.empty?

        parsed = JSON.parse(raw_content)

        usage = response["usage"] || {}
        metrics = StepMetrics.new(
          latency_ms:    (end_time - start_time).to_i,
          input_tokens:  usage["prompt_tokens"] || 0,
          output_tokens: usage["completion_tokens"] || 0
        )

        @log.info("llm response", {
          model: model,
          latency_ms: metrics.latency_ms,
          input_tokens: metrics.input_tokens,
          output_tokens: metrics.output_tokens
        })

        { output: parsed, metrics: metrics }
      rescue JSON::ParserError => e
        raise LLMError, "Failed to parse LLM JSON response: #{e.message}"
      end

      private

      def client
        @client ||= OpenAI::Client.new(access_token: ENV.fetch("OPENAI_API_KEY") {
          raise LLMError, "OPENAI_API_KEY environment variable is not set"
        })
      end

      def build_messages(system_prompt:, user_content:)
        messages = []
        messages << { role: "system", content: system_prompt } if system_prompt && !system_prompt.empty?
        messages << { role: "user", content: user_content }
        messages
      end
    end
  end
end
