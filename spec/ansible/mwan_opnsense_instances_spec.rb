# frozen_string_literal: true

require 'English'
require 'tmpdir'
require_relative '../support/command_runner'

# The OPNsense deploy runs mwan-opnsense-instances.sh on the router to stop
# every daemon instance before it swaps the binary, and to confirm exactly one
# runs afterwards. An instance the rc.d pidfile does not name is invisible to
# rc.d, so the script finds instances by command line instead.
#
# These checks run the real script against real processes. Each fake instance is
# a sleep started with the argv[0] a daemon(8) supervisor or an mwan-opnsense
# daemon shows in ps, under paths inside the test's own temp directory, so the
# script's matching, signalling, and escalation run for real and never reach a
# process outside the test.
module MwanOpnsenseInstances
  REPOSITORY_ROOT = File.expand_path('../..', __dir__)
  SCRIPT = File.join(REPOSITORY_ROOT, 'ansible', 'playbooks', 'files', 'mwan-opnsense-instances.sh')

  FAKE_SLEEP_SECONDS = '300'
  WAIT_SECONDS = '2'
  RUN_TIMEOUT_SECONDS = 90
  PROCESS_POLL_INTERVAL_SECONDS = 0.05
  PROCESS_POLL_TIMEOUT_SECONDS = 10

  EXIT_FAILURE = 1
  SCRIPT_MODE = 0o700
  PIDFILE_MODE = 0o600

  # One test's fake guest paths. Nothing exists at run_shim or daemon_binary;
  # those paths only appear in the fake processes' command lines.
  class Layout
    attr_reader :run_shim, :daemon_binary, :pidfile, :rc_script, :rc_log

    def initialize(directory)
      @run_shim = File.join(directory, 'libexec', 'mwan-opnsense-run')
      @daemon_binary = File.join(directory, 'sbin', 'mwan-opnsense')
      @pidfile = File.join(directory, 'mwan_opnsense.pid')
      @rc_script = File.join(directory, 'rc.d-mwan_opnsense')
      @rc_log = File.join(directory, 'rc.log')
      write_rc_stand_in
    end

    def write_pidfile(pid)
      File.write(@pidfile, pid.to_s, perm: PIDFILE_MODE)
    end

    def rc_verbs
      return [] unless File.exist?(@rc_log)

      File.read(@rc_log).split("\n").map(&:strip).reject(&:empty?)
    end

    private

    # The rc.d stand-in records its verb and exits 1, the way the real stop does
    # on an invalid pidfile, so the sweep cannot lean on the rc.d stop.
    def write_rc_stand_in
      File.write(@rc_script, "#!/bin/sh\necho \"$1\" >> \"#{@rc_log}\"\nexit 1\n")
      File.chmod(SCRIPT_MODE, @rc_script)
    end
  end

  # Starts and reaps the fake instances one test needs.
  class Processes
    def initialize
      @pids = []
    end

    # Starts one process whose ps command line begins with argv0, and waits
    # until ps shows it. With ignore_term the process ignores SIGTERM, like a
    # daemon stuck in a serial write, so only SIGKILL ends it. An ignored
    # disposition survives exec, which is what makes the fake stubborn.
    def start_fake(argv0, ignore_term: false)
      script = %(exec -a "$0" sleep #{FAKE_SLEEP_SECONDS})
      script = %(trap "" TERM; #{script}) if ignore_term
      pid = Process.spawn('bash', '-c', script, argv0)
      @pids << pid
      MwanOpnsenseInstances.wait_for_command_prefix(pid, argv0)
      pid
    end

    # Starts a fake supervisor and a fake daemon whose parent it is, and returns
    # both pids. Unlike daemon(8), the fake supervisor does not forward SIGTERM,
    # so a TERM leaves its daemon orphaned, which is the shape the deploy must
    # still sweep.
    def start_fake_supervisor(supervisor_argv0, daemon_argv0)
      script = %(exec -a "$1" sleep #{FAKE_SLEEP_SECONDS} >/dev/null 2>&1 & echo "$!"; ) +
               %(exec -a "$0" sleep #{FAKE_SLEEP_SECONDS})
      reader, writer = IO.pipe
      supervisor_pid = Process.spawn('bash', '-c', script, supervisor_argv0, daemon_argv0, out: writer)
      writer.close
      daemon_pid = Integer(reader.gets.to_s.strip)
      reader.close
      @pids.push(supervisor_pid, daemon_pid)
      MwanOpnsenseInstances.wait_for_command_prefix(supervisor_pid, supervisor_argv0)
      MwanOpnsenseInstances.wait_for_command_prefix(daemon_pid, daemon_argv0)
      [supervisor_pid, daemon_pid]
    end

    def reap
      @pids.each do |pid|
        Process.kill('KILL', pid)
      rescue Errno::ESRCH
        nil
      end
      @pids.each do |pid|
        Process.wait(pid)
      rescue Errno::ECHILD
        nil
      end
      @pids.clear
    end
  end

  module_function

  # Runs the real script against the real ps.
  def run(*args)
    argv = ['/bin/sh', SCRIPT, *args]
    CommandRunner.capture(argv, stdin_data: '', chdir: REPOSITORY_ROOT,
                                timeout_seconds: RUN_TIMEOUT_SECONDS)
  end

  # The ps command line of pid, and whether pid is a live process. A zombie
  # counts as gone. The width flags matter: without them procps truncates the
  # command to the terminal width, and these command lines are temp-directory
  # paths long enough to lose their tail on a CI runner.
  def process_command(pid)
    output = `ps -ww -o stat= -o command= -p #{Integer(pid)} 2>/dev/null`
    return [nil, false] unless $CHILD_STATUS.nil? || $CHILD_STATUS.success?

    fields = output.strip.split(' ', 2)
    return [nil, false] if fields.length < 2 || fields.first.start_with?('Z')

    [fields.last.strip, true]
  end

  def running?(pid)
    process_command(pid).last
  end

  def wait_for_command_prefix(pid, prefix)
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + PROCESS_POLL_TIMEOUT_SECONDS
    loop do
      command, running = process_command(pid)
      return if running && command.start_with?(prefix)
      raise "pid #{pid} never showed command #{prefix.inspect} (last #{command.inspect})" if
        Process.clock_gettime(Process::CLOCK_MONOTONIC) > deadline

      sleep PROCESS_POLL_INTERVAL_SECONDS
    end
  end

  # The pid of a process that has already exited, which no live process owns.
  def exited_pid
    pid = Process.spawn('true')
    Process.wait(pid)
    pid
  end
end

RSpec.describe MwanOpnsenseInstances do
  around do |example|
    Dir.mktmpdir('mwan-opnsense-instances') do |work_directory|
      @layout = MwanOpnsenseInstances::Layout.new(work_directory)
      @processes = MwanOpnsenseInstances::Processes.new
      begin
        example.run
      ensure
        @processes.reap
      end
    end
  end

  describe 'stop' do
    # The production failure: the instance the pidfile tracks, a supervisor from
    # an older rc.d script that the pidfile no longer names, and a daemon that
    # ignores SIGTERM all run at once. stop must end every one of them, escalate
    # to KILL for the stubborn daemon, and leave a process that merely names the
    # daemon path alone.
    it 'ends every instance' do
      tracked_supervisor, tracked_daemon = @processes.start_fake_supervisor(
        "daemon: #{@layout.run_shim}[1] (daemon)", @layout.daemon_binary
      )
      old_supervisor, old_daemon = @processes.start_fake_supervisor(
        "daemon: #{@layout.daemon_binary}[1] (daemon)", "/bin/sh #{@layout.run_shim}"
      )
      stubborn_daemon = @processes.start_fake("#{@layout.daemon_binary}.current", ignore_term: true)
      bystander = @processes.start_fake("less #{@layout.daemon_binary}")
      @layout.write_pidfile(tracked_supervisor)

      result = MwanOpnsenseInstances.run('stop', @layout.rc_script, @layout.run_shim,
                                         @layout.daemon_binary, MwanOpnsenseInstances::WAIT_SECONDS)

      expect(result.timed_out).to be(false), "stop did not finish:\n#{result.error_output}"
      expect(result.exit_status.exitstatus).to eq(0),
                                              "stop exit code = #{result.exit_status.exitstatus.inspect}, want 0\n#{result.error_output}"
      expect(@layout.rc_verbs).to eq(['stop']),
                                  "rc.d stand-in log = #{@layout.rc_verbs.inspect}, want one stop"
      {
        'tracked supervisor' => tracked_supervisor, 'tracked daemon' => tracked_daemon,
        'old supervisor' => old_supervisor, 'old daemon' => old_daemon,
        'stubborn daemon' => stubborn_daemon
      }.each do |name, pid|
        expect(MwanOpnsenseInstances.running?(pid)).to be(false),
                                                       "#{name} pid #{pid} still runs after stop\n#{result.error_output}"
      end
      expect(result.error_output).to include("sending KILL to daemon pids #{stubborn_daemon}"),
                                     "stop did not escalate to KILL for the stubborn daemon\n#{result.error_output}"
      expect(MwanOpnsenseInstances.running?(bystander)).to be(true),
                                                           "stop ended pid #{bystander}, which only names the daemon path"
    end
  end

  describe 'check-one' do
    it 'accepts one instance the pidfile names' do
      supervisor, daemon = @processes.start_fake_supervisor(
        "daemon: #{@layout.run_shim}[1] (daemon)", @layout.daemon_binary
      )
      @layout.write_pidfile(supervisor)

      result = MwanOpnsenseInstances.run('check-one', @layout.run_shim,
                                         @layout.daemon_binary, @layout.pidfile)

      expect(result.timed_out).to be(false), "check-one did not finish:\n#{result.error_output}"
      expect(result.exit_status.exitstatus).to eq(0),
                                              "check-one exit code = #{result.exit_status.exitstatus.inspect}, want 0\n#{result.error_output}"
      expect(result.output).to eq("supervisor=#{supervisor} daemon=#{daemon}\n"),
                               "check-one stdout = #{result.output.inspect}"
    end

    # A second instance the pidfile does not name is exactly what rc.d cannot
    # see, and starting another reader on the serial port is the failure this
    # check exists to prevent.
    it 'refuses a second instance the pidfile does not name' do
      tracked, = @processes.start_fake_supervisor(
        "daemon: #{@layout.run_shim}[1] (daemon)", @layout.daemon_binary
      )
      @processes.start_fake_supervisor(
        "daemon: #{@layout.daemon_binary}[1] (daemon)", @layout.daemon_binary
      )
      @layout.write_pidfile(tracked)

      result = MwanOpnsenseInstances.run('check-one', @layout.run_shim,
                                         @layout.daemon_binary, @layout.pidfile)

      expect(result.timed_out).to be(false), "check-one did not finish:\n#{result.error_output}"
      expect(result.exit_status.exitstatus).to eq(MwanOpnsenseInstances::EXIT_FAILURE),
                                              "check-one exit code = #{result.exit_status.exitstatus.inspect}, want 1\n#{result.error_output}"
      expect(result.error_output).to include('found 2 supervisor(s) and 2 daemon(s)'),
                                     "check-one did not report both instances\n#{result.error_output}"
    end

    it 'refuses one instance the pidfile does not name' do
      @processes.start_fake_supervisor(
        "daemon: #{@layout.run_shim}[1] (daemon)", @layout.daemon_binary
      )
      @layout.write_pidfile(MwanOpnsenseInstances.exited_pid)

      result = MwanOpnsenseInstances.run('check-one', @layout.run_shim,
                                         @layout.daemon_binary, @layout.pidfile)

      expect(result.timed_out).to be(false), "check-one did not finish:\n#{result.error_output}"
      expect(result.exit_status.exitstatus).to eq(MwanOpnsenseInstances::EXIT_FAILURE),
                                              "check-one exit code = #{result.exit_status.exitstatus.inspect}, want 1\n#{result.error_output}"
    end

    it 'refuses when no instance runs' do
      result = MwanOpnsenseInstances.run('check-one', @layout.run_shim,
                                         @layout.daemon_binary, @layout.pidfile)

      expect(result.timed_out).to be(false), "check-one did not finish:\n#{result.error_output}"
      expect(result.exit_status.exitstatus).to eq(MwanOpnsenseInstances::EXIT_FAILURE),
                                              "check-one exit code = #{result.exit_status.exitstatus.inspect}, want 1\n#{result.error_output}"
      expect(result.error_output).to include('found 0 supervisor(s) and 0 daemon(s)'),
                                     "check-one did not report zero instances\n#{result.error_output}"
    end
  end
end
