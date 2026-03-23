# frozen_string_literal: true

module AgenticEngine
  # Lightweight structured logger used throughout the engine.
  #
  # Usage:
  #   log = AgenticEngine::Logger.create("orchestrator")
  #   log.info("workflow started", { workflow: "product-review" })
  class Logger
    LEVELS = %w[debug info warn error].freeze
    LEVEL_COLORS = { "debug" => "\e[36m", "info" => "\e[32m", "warn" => "\e[33m", "error" => "\e[31m" }.freeze
    RESET = "\e[0m"

    attr_reader :component

    # Create a logger for a named component.
    #
    # @param component [String] e.g. "orchestrator", "runner", "llm"
    # @return [Logger]
    def self.create(component)
      new(component)
    end

    def initialize(component)
      @component = component
      @min_level = ENV.fetch("AGENTIC_LOG_LEVEL", "info")
    end

    LEVELS.each do |level|
      define_method(level) do |message, data = nil|
        return unless should_log?(level)

        timestamp = Time.now.utc.strftime("%Y-%m-%dT%H:%M:%S.%3NZ")
        color = LEVEL_COLORS[level]
        parts = ["#{color}[#{timestamp}]#{RESET}", "#{color}#{level.upcase.ljust(5)}#{RESET}", "(#{@component})", message]
        parts << data.inspect if data
        $stderr.puts parts.join(" ")
      end
    end

    # Serialize an exception into a hash suitable for structured logging / JSON.
    #
    # @param err [Exception]
    # @return [Hash]
    def self.serialize_error(err)
      {
        class: err.class.name,
        message: err.message,
        backtrace: err.backtrace&.first(10)
      }
    end

    private

    def should_log?(level)
      LEVELS.index(level) >= LEVELS.index(@min_level)
    end
  end
end
