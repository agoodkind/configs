# frozen_string_literal: true

module MwanAcceptance
  ProcessResult = Data.define(:stdout, :stderr, :status)

  # Process groups enforce deadlines and preserve command evidence.
  class Processes
    attr_reader :interrupted

    def initialize(directory)
      @directory = directory
      @children = {}
      @sequence = 0
      @interrupted = false
    end

    def interrupt
      @interrupted = true
    end

    def start(argv, label)
      @sequence += 1
      stem = File.join(@directory, format('%<sequence>04d-%<label>s', sequence: @sequence, label: label))
      File.write("#{stem}.command.json", JSON.generate(argv))
      output = File.open("#{stem}.stdout", 'wb')
      error = File.open("#{stem}.stderr", 'wb')
      pid = Process.spawn(*argv, pgroup: true, out: output, err: error)
      @children[pid] = stem
      [pid, stem]
    ensure
      output&.close
      error&.close
    end

    def run(argv, label, seconds, cleanup: false)
      unless cleanup
        check_deadline
        seconds = [seconds, @deadline - monotonic].min if @deadline
      end
      pid, stem = start(argv, label)
      status = wait(pid, seconds, cleanup: cleanup)
      check_deadline unless cleanup
      result = ProcessResult.new(stdout: File.binread("#{stem}.stdout"), stderr: File.binread("#{stem}.stderr"), status: status)
      raise Failure, "#{label} failed (#{status.exitstatus}): #{result.stderr[-2000..] || result.stderr}" unless status.success?

      result
    end

    def with_deadline(deadline)
      previous = @deadline
      @deadline = previous ? [previous, deadline].min : deadline
      yield
    ensure
      @deadline = previous
    end

    def check_deadline
      raise Failure, 'acceptance observation exceeded its deadline' if @deadline && monotonic >= @deadline
    end

    def wait(pid, seconds, cleanup: false)
      deadline = monotonic + seconds
      loop do
        raise Interrupted, 'acceptance interrupted' if @interrupted && !cleanup

        pair = Process.wait2(pid, Process::WNOHANG)
        if pair
          record_status(pid, pair[1], cleanup)
          @children.delete(pid)
          return pair[1]
        end
        raise Failure, "process #{pid} exceeded #{seconds}s deadline" if monotonic >= deadline

        sleep 0.05
      end
    end

    def running?(pid)
      Process.kill(0, pid)
      true
    rescue Errno::ESRCH
      false
    end

    def cleanup
      groups = @children.keys
      groups.each do |pid|
        reap_child?(pid, Process::WNOHANG)
        signal_group(pid, 'TERM')
      end
      sleep 0.1 unless groups.empty?
      groups.each do |pid|
        reap_child?(pid, Process::WNOHANG) if @children.key?(pid)
        signal_group(pid, 'KILL')
        reap_child?(pid, 0) if @children.key?(pid)
      end
    end

    def monotonic
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end

    private

    def reap_child?(pid, flags)
      pair = Process.wait2(pid, flags)
      return false unless pair

      record_status(pid, pair[1], true)
      @children.delete(pid)
      true
    end

    def signal_group(pid, signal)
      Process.kill(signal, -pid)
    rescue Errno::ESRCH
      nil
    end

    def record_status(pid, status, cleanup)
      stem = @children.fetch(pid)
      File.write("#{stem}.status.json", JSON.generate({ pid: pid, exit_status: status.exitstatus, signal: status.termsig, cleanup: cleanup, ended: Time.now.iso8601(9) }))
    end
  end

  # SSH commands use the declared Linux or Proxmox identity adapter.
  class Remote
    def initialize(plan, processes)
      @plan = plan
      @processes = processes
    end

    def prefix(target)
      return ['pct', 'exec', target.vmid.to_s, '--'] if target.adapter == 'proxmox'
      return ['ip', 'netns', 'exec', target.namespace] if target.namespace

      []
    end

    def argv(target, command, guest: true)
      command = prefix(target) + command if guest
      ['ssh', '-F', @plan.ssh_config, '-o', 'BatchMode=yes', '-o', "ConnectTimeout=#{@plan.timeout_seconds}", target.host, Shellwords.join(command)]
    end

    def read(target, command, label, guest: true, cleanup: false)
      @processes.run(argv(target, command, guest: guest), label, @plan.timeout_seconds, cleanup: cleanup).stdout
    end

    def json(target, command, label)
      JSON.parse(read(target, command, label))
    rescue JSON::ParserError => e
      raise Failure, "#{label} returned invalid JSON: #{e.message}"
    end

    def identity(target)
      hostname = read(target, ['hostname'], "#{target.id}-hostname").strip
      raise Failure, "#{target.id}: hostname #{hostname} differs from #{target.hostname}" unless hostname == target.hostname

      if target.adapter == 'proxmox'
        configuration = read(target, ['pct', 'config', target.vmid.to_s], "#{target.id}-pct-config", guest: false)
        raise Failure, "#{target.id}: pct hostname differs" unless configuration.lines.include?("hostname: #{target.pve_hostname}\n")
      else
        identity = read(target, ['cat', '/etc/machine-id'], "#{target.id}-machine-id").strip
        raise Failure, "#{target.id}: machine identity differs" unless identity == target.machine_id
      end
    end
  end

  Observer = Data.define(:target, :unit, :pid, :capture_pid, :stem, :label)

  # Packet observers use bounded systemd units and require kernel drop counters.
  class Captures
    attr_reader :observers

    def initialize(remote, processes, plan, run_id)
      @remote = remote
      @processes = processes
      @plan = plan
      @run_id = run_id
      @observers = []
      @finished = {}
      @runner_status = {}
    end

    def start(target, interface, label, port)
      unit = "mwan-acceptance-#{@run_id}-#{label}"
      command = ['systemd-run', '--quiet', '--collect', '--pipe', '--wait', "--unit=#{unit}", "--property=RuntimeMaxSec=#{@plan.capture_seconds}",
                 '--property=KillSignal=SIGINT', '--property=TimeoutStopSec=3', '--']
      guest = target.adapter == 'proxmox'
      command += @remote.prefix(target) unless guest
      command += ['tcpdump', '--immediate-mode', '-U', '-nn', '-s', '0', '-i', interface, '-w', '-', 'tcp', 'port', port.to_s]
      pid, stem = @processes.start(@remote.argv(target, command, guest: guest), label)
      observer = Observer.new(target: target, unit: unit, pid: pid, capture_pid: nil, stem: stem, label: label)
      @observers.push(observer)
      deadline = @processes.monotonic + @plan.timeout_seconds
      loop do
        raise Interrupted, 'acceptance interrupted' if @processes.interrupted

        if File.read("#{stem}.stderr").include?('listening on')
          observer = observer.with(capture_pid: capture_pid(observer))
          @observers[-1] = observer
          return observer
        end
        raise Failure, "#{label}: capture did not start" unless @processes.running?(pid) && @processes.monotonic < deadline

        sleep 0.05
      end
    end

    def stop
      errors = []
      @observers.each do |observer|
        stop_one(observer)
      rescue Failure => e
        errors.push(e.message)
      end
      raise Failure, errors.join("\n") unless errors.empty?
    end

    def packets(observer)
      @processes.run(['tcpdump', '-nn', '-tt', '-S', '-r', "#{observer.stem}.stdout"], "#{observer.label}-decode", @plan.timeout_seconds).stdout
    end

    private

    def capture_pid(observer, cleanup: false)
      guest = observer.target.adapter == 'proxmox'
      command = ['systemctl', 'show', observer.unit, '--property=Id,LoadState,ActiveState,MainPID']
      state = @remote.read(observer.target, command, "#{observer.label}-active", guest: guest, cleanup: cleanup).lines.to_h { |line| line.strip.split('=', 2) }
      unless state['Id'] == "#{observer.unit}.service" && state['LoadState'] == 'loaded' && state['ActiveState'] == 'active' && /\A[1-9]\d*\z/.match?(state['MainPID'].to_s)
        raise Failure, "#{observer.label}: capture unit is not active with a process: #{state}"
      end

      pid = Integer(state.fetch('MainPID'))
      executable = @remote.read(observer.target, ['readlink', "/proc/#{pid}/exe"], "#{observer.label}-executable", guest: guest, cleanup: cleanup).strip
      raise Failure, "#{observer.label}: capture process is #{executable}" unless File.basename(executable) == 'tcpdump'

      File.write("#{observer.stem}.capture.json", JSON.generate({ unit: observer.unit, pid: pid, executable: executable, guest: guest }))
      pid
    end

    def stop_one(observer)
      return if @finished[observer.unit]

      guest = observer.target.adapter == 'proxmox'
      errors = []
      begin
        pid = observer.capture_pid || capture_pid(observer, cleanup: true)
      rescue Failure => e
        errors.push(e.message)
      end
      stop_runner(observer, errors)
      raise Failure, errors.join("\n") unless errors.empty?

      counters = File.read("#{observer.stem}.stderr")
      raise Failure, "#{observer.label}: capture omitted kernel drop counters" unless counters.match?(/^0 packets dropped by kernel$/)

      command = ['find', '/proc', '-maxdepth', '1', '-mindepth', '1', '-name', pid.to_s, '-print']
      remaining = @remote.read(observer.target, command, "#{observer.label}-process-absent", guest: guest, cleanup: true)
      raise Failure, "#{observer.label}: capture process #{pid} remains" unless remaining.empty?

      @finished[observer.unit] = true
    end

    def stop_runner(observer, errors)
      begin
        @remote.read(observer.target, ['systemctl', 'stop', observer.unit], "#{observer.label}-stop", guest: observer.target.adapter == 'proxmox', cleanup: true)
      rescue Failure => e
        errors.push(e.message)
      end
      begin
        status = @runner_status[observer.unit] ||= @processes.wait(observer.pid, @plan.timeout_seconds, cleanup: true)
        raise Failure, "#{observer.label}: capture exited #{status.exitstatus}" unless status.success?
      rescue Failure => e
        errors.push(e.message)
      end
    end
  end
end
