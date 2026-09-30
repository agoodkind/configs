# frozen_string_literal: true

module MwanAcceptance
  # Persistent failure records must remain readable after an observed restart.
  class HistoryObserver
    def initialize(remote, plan, product, processes, directory)
      @remote = remote
      @plan = plan
      @product = product
      @processes = processes
      @directory = directory
    end

    def verify
      before_pid = pid
      raise Failure, 'product service has no running main process' unless before_pid.positive?

      started = Time.iso8601(@remote.read(@plan.gateway, ['date', '--iso-8601=ns'], 'history-clock').strip.tr(',', '.'))
      deadline = @processes.monotonic + @plan.history.seconds
      records = []
      restarted = false
      File.write(File.join(@directory, 'phase.json'), JSON.generate({ phase: 'observing-failure-and-restart', started: started.iso8601(9), initial_pid: before_pid }))
      loop do
        raise Interrupted, 'acceptance interrupted' if @processes.interrupted

        current = history
        records |= current.select { |record| relevant?(record, started) }
        @product.snapshot
        current_pid = pid
        restarted ||= current_pid.positive? && current_pid != before_pid
        if restarted && records.any?
          missing = records.reject { |record| current.include?(record) }
          raise Failure, 'product transition records disappeared after restart' unless missing.empty?

          return { restart_observed: true, retained_transitions: records }
        end
        raise Failure, 'product restart and matching persistent failure history were not observed' if @processes.monotonic >= deadline

        sleep 0.2
      end
    end

    private

    def pid
      @remote.read(@plan.gateway, ['systemctl', 'show', @plan.history.service, '--property=MainPID', '--value'], 'product-main-pid').strip.to_i
    end

    def history
      attempts = 0
      begin
        read_history
      rescue Failure => e
        raise unless e.message.include?('product-history-records failed (1): cat:') && e.message.include?('No such file or directory')
        raise if attempts.positive?

        attempts += 1
        retry
      end
    end

    def read_history
      settings = @plan.history
      paths = @remote.read(@plan.gateway, ['find', settings.directory, '-maxdepth', '1', '-type', 'f', '-name', settings.pattern], 'product-history-paths').lines.map(&:strip).sort
      raise Failure, 'product history files missing' if paths.empty?

      paths.flat_map do |path|
        output = @remote.read(@plan.gateway, ['cat', path], 'product-history-records')
        output.lines.filter_map do |line|
          next if line.strip.empty?

          JSON.parse(line)
        end
      end
    rescue JSON::ParserError => e
      raise Failure, "product history is not readable JSON: #{e.message}"
    end

    def relevant?(record, started)
      transition = record['transition']
      return false unless transition
      return false unless record['connection_id'] == @plan.history.connection
      return false unless %w[not-ready failed].include?(transition.fetch('Current'))

      transition.fetch('Family') == @plan.history.family && transition.fetch('Dependency') == @plan.history.dependency &&
        transition.fetch('Reason') == @plan.history.reason && Time.iso8601(transition.fetch('At')) >= started
    end
  end
end
