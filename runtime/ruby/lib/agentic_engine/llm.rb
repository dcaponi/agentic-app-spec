# frozen_string_literal: true

require "openai"
require "json"
require "net/http"
require "uri"

module AgenticEngine
  # LLM interaction layer supporting OpenAI and Anthropic providers.
  # Uses the ruby-openai gem for OpenAI and raw Net::HTTP for Anthropic.
  module LLM
    class LLMError < StandardError; end

    ANTHROPIC_ENDPOINT = "https://api.anthropic.com/v1/messages"

    @log = Logger.create("llm")

    class << self
      # Call an LLM via the appropriate provider.
      #
      # @param model [String] e.g. "gpt-4.1" or "claude-sonnet-4-5-20241022"
      # @param system_prompt [String, nil] system message content
      # @param user_content [String] user message content
      # @param temperature [Float] sampling temperature (default 0.7)
      # @param schema_name [String, nil] if non-nil, load the named schema and
      #   request structured output; otherwise fall back to JSON mode.
      # @param base_url [String, nil] optional OpenAI-compatible base URL
      # @param api_key_env [String, nil] env var name holding the API key for base_url
      # @return [Hash] { output: <parsed JSON>, metrics: StepMetrics }
      def call_llm(model:, system_prompt: nil, user_content:, temperature: 0.7, schema_name: nil, base_url: nil, api_key_env: nil)
        if base_url
          @log.debug("call_llm", { provider: "openai-compatible", base_url: base_url, model: model, schema: schema_name, temperature: temperature })
          call_openai(
            model: model,
            system_prompt: system_prompt,
            user_content: user_content,
            temperature: temperature,
            schema_name: schema_name,
            base_url: base_url,
            api_key_env: api_key_env
          )
        elsif model&.start_with?("claude-")
          @log.debug("call_llm", { provider: "anthropic", model: model, schema: schema_name, temperature: temperature })
          call_anthropic(
            model: model,
            system_prompt: system_prompt,
            user_content: user_content,
            temperature: temperature,
            schema_name: schema_name
          )
        else
          @log.debug("call_llm", { provider: "openai", model: model, schema: schema_name, temperature: temperature })
          call_openai(
            model: model,
            system_prompt: system_prompt,
            user_content: user_content,
            temperature: temperature,
            schema_name: schema_name
          )
        end
      end

      private

      # ── OpenAI implementation ──

      def call_openai(model:, system_prompt:, user_content:, temperature:, schema_name:, base_url: nil, api_key_env: nil)
        messages = build_openai_messages(system_prompt: system_prompt, user_content: user_content)
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

        client = if base_url
                   api_key = api_key_env ? ENV[api_key_env] || "not-needed" : "not-needed"
                   OpenAI::Client.new(access_token: api_key, uri_base: base_url)
                 else
                   openai_client
                 end

        start_time = Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond)
        response = client.chat(parameters: params)
        end_time = Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond)

        choice = response.dig("choices", 0)
        raise LLMError, "No choices returned from OpenAI" unless choice

        finish_reason = choice["finish_reason"]
        if finish_reason == "refusal"
          refusal = choice.dig("message", "refusal")
          raise LLMError, "OpenAI refused request: #{refusal}"
        end

        raw_content = choice.dig("message", "content")
        raise LLMError, "Empty content from OpenAI" if raw_content.nil? || raw_content.strip.empty?

        parsed = JSON.parse(raw_content)

        usage = response["usage"] || {}
        metrics = StepMetrics.new(
          latency_ms:    (end_time - start_time).to_i,
          input_tokens:  usage["prompt_tokens"] || 0,
          output_tokens: usage["completion_tokens"] || 0
        )

        @log.info("OpenAI response", {
          model: model,
          latency_ms: metrics.latency_ms,
          input_tokens: metrics.input_tokens,
          output_tokens: metrics.output_tokens
        })

        { output: parsed, metrics: metrics }
      rescue JSON::ParserError => e
        raise LLMError, "Failed to parse OpenAI JSON response: #{e.message}"
      end

      # ── Anthropic implementation ──

      def call_anthropic(model:, system_prompt:, user_content:, temperature:, schema_name:)
        system = build_anthropic_system_prompt(system_prompt, schema_name)

        body = {
          model: model,
          max_tokens: 4096,
          system: system,
          messages: [{ role: "user", content: user_content }],
          temperature: temperature
        }

        start_time = Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond)

        uri = URI.parse(ANTHROPIC_ENDPOINT)
        http = Net::HTTP.new(uri.host, uri.port)
        http.use_ssl = true

        request = Net::HTTP::Post.new(uri.path)
        request["Content-Type"] = "application/json"
        request["x-api-key"] = anthropic_api_key
        request["anthropic-version"] = "2023-06-01"
        request.body = JSON.generate(body)

        response = http.request(request)
        end_time = Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond)

        unless response.is_a?(Net::HTTPSuccess)
          raise LLMError, "Anthropic API returned status #{response.code}: #{response.body}"
        end

        parsed_response = JSON.parse(response.body)

        # Extract text from content blocks
        text_block = (parsed_response["content"] || []).find { |b| b["type"] == "text" }
        raise LLMError, "No text content in Anthropic response" unless text_block

        raw_content = text_block["text"]
        raise LLMError, "Empty text content from Anthropic" if raw_content.nil? || raw_content.strip.empty?

        parsed = JSON.parse(raw_content)

        usage = parsed_response["usage"] || {}
        metrics = StepMetrics.new(
          latency_ms:    (end_time - start_time).to_i,
          input_tokens:  usage["input_tokens"] || 0,
          output_tokens: usage["output_tokens"] || 0
        )

        @log.info("Anthropic response", {
          model: model,
          latency_ms: metrics.latency_ms,
          input_tokens: metrics.input_tokens,
          output_tokens: metrics.output_tokens
        })

        { output: parsed, metrics: metrics }
      rescue JSON::ParserError => e
        raise LLMError, "Failed to parse Anthropic JSON response: #{e.message}"
      end

      # ── Helpers ──

      def openai_client
        @openai_client ||= OpenAI::Client.new(access_token: ENV.fetch("OPENAI_API_KEY") {
          raise LLMError, "OPENAI_API_KEY environment variable is not set"
        })
      end

      def anthropic_api_key
        @anthropic_api_key ||= ENV.fetch("ANTHROPIC_API_KEY") {
          raise LLMError, "ANTHROPIC_API_KEY environment variable is not set"
        }
      end

      def build_openai_messages(system_prompt:, user_content:)
        messages = []
        messages << { role: "system", content: system_prompt } if system_prompt && !system_prompt.empty?
        messages << { role: "user", content: user_content }
        messages
      end

      def build_anthropic_system_prompt(base, schema_name)
        base_prompt = base || ""

        if schema_name
          begin
            schema_def = Loader.load_schema(schema_name)
            schema_body = schema_def["schema"] || schema_def
            schema_json = JSON.pretty_generate(schema_body)
            return "#{base_prompt}\n\nYou must respond with a valid JSON object that conforms to the following JSON schema:\n#{schema_json}\n\nOutput only the JSON with no additional text, markdown, or code fences."
          rescue StandardError
            # Fall through to basic JSON instruction
          end
        end

        "#{base_prompt}\n\nYou must respond with a valid JSON object. Output only the JSON with no additional text, markdown, or code fences."
      end
    end
  end
end
