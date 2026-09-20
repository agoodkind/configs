# frozen_string_literal: true

require 'fileutils'
require 'json'
require 'tmpdir'
require_relative '../support/command_runner'

# The MWAN deploy starts its gate as a transient unit on the Proxmox host, then
# reboots the gateway. collect-deploy-verdict.sh is how the controller learns
# what happened afterwards, so its probe ordering decides whether a healthy
# deploy reports success. These checks run the real script with a fake ssh first
# on PATH; nothing reaches a host.
module CollectDeployVerdict
  REPOSITORY_ROOT = File.expand_path('../..', __dir__)
  SCRIPT = File.join(REPOSITORY_ROOT, 'ansible', 'playbooks', 'files', 'collect-deploy-verdict.sh')
  FIXTURE_DIRECTORY = File.join(REPOSITORY_ROOT, 'spec', 'fixtures', 'collect_deploy_verdict')
  FAKE_SSH = 'fake-ssh.sh'

  TRACE_ID = 'trace-1'
  OLD_BOOT_ID = 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'
  DEFAULT_VERDICT_PATH = "/run/mwan-deploy-gate/#{TRACE_ID}.json"
  ADDRESS = '192.0.2.10'

  # The fast cases exit on their first pass, so the budget never elapses. The
  # timeout case is the exception and names its own below.
  BUDGET_SECONDS = 30
  # The transport case runs to expiry on purpose. Twelve seconds is two passes
  # at the script's ten-second poll, which is what proves it kept polling rather
  # than ruling on the first transport failure.
  TIMEOUT_BUDGET_SECONDS = 12
  RUN_TIMEOUT_SECONDS = 90

  FAKE_MODE = 0o700

  EXIT_TIMED_OUT = 1
  EXIT_GATE_DEATH = 2

  NO_FAILURES = '0'
  ONE_FAILURE = '1'
  NEVER_APPEARS = '100'
  NO_TRANSPORT_FAILURE = '0'

  # One run's fake PATH, staged verdict, and state directory. The fake ssh
  # records the remote command it was handed there, so a test can read back what
  # the script actually asked the host to run.
  class Harness
    attr_reader :state_directory

    def initialize(work_directory)
      @state_directory = work_directory
      @fake_bin = File.join(work_directory, 'bin')
      FileUtils.mkdir_p(@fake_bin)
      target = File.join(@fake_bin, 'ssh')
      FileUtils.cp(File.join(FIXTURE_DIRECTORY, FAKE_SSH), target)
      File.chmod(FAKE_MODE, target)
    end

    # Stages the verdict the fake ssh serves once its failure budget is spent.
    def stage_verdict(verdict)
      File.write(File.join(@state_directory, 'verdict.json'), JSON.generate(verdict))
    end

    # Runs the real collector. cat_failures is the number of leading verdict
    # reads that fail with ENOENT; reads past transport_after fail with ssh's
    # 255 instead of reaching the host.
    def run(verdict_path: DEFAULT_VERDICT_PATH, cat_failures: NO_FAILURES,
            transport_after: NO_TRANSPORT_FAILURE, budget_seconds: BUDGET_SECONDS)
      argv = [
        'env',
        "PATH=#{@fake_bin}#{File::PATH_SEPARATOR}#{ENV.fetch('PATH')}",
        "FAKE_SSH_STATE_DIR=#{@state_directory}",
        "FAKE_SSH_CAT_FAILURES=#{cat_failures}",
        "FAKE_SSH_TRANSPORT_AFTER=#{transport_after}",
        SCRIPT,
        verdict_path,
        "mwan-deploy-gate-#{TRACE_ID}",
        TRACE_ID,
        OLD_BOOT_ID,
        budget_seconds.to_s,
        ADDRESS
      ]
      CommandRunner.capture(argv, stdin_data: '', chdir: REPOSITORY_ROOT,
                                  timeout_seconds: RUN_TIMEOUT_SECONDS)
    end

    # The remote command the fake ssh last recorded, or nil when none ran.
    def last_remote_read
      path = File.join(@state_directory, 'last-cat-command')
      return nil unless File.exist?(path)

      File.read(path)
    end
  end

  module_function

  def verdict(trace_id: TRACE_ID, old_boot_id: OLD_BOOT_ID)
    {
      'trace_id' => trace_id,
      'old_boot_id' => old_boot_id,
      'reboot_rc' => 0,
      'egress_rc' => 0,
      'started_at' => '2026-08-29T18:35:58Z',
      'finished_at' => '2026-08-29T18:37:12Z'
    }
  end
end

RSpec.describe CollectDeployVerdict do
  around do |example|
    Dir.mktmpdir('collect-deploy-verdict') do |work_directory|
      @harness = CollectDeployVerdict::Harness.new(work_directory)
      example.run
    end
  end

  # The reproduced race, and the reason this script exists: the read fails while
  # the gate is still running, then the gate records its verdict and exits, and
  # systemd-run --collect garbage-collects the unit so the status probe reports
  # inactive. The collector must re-read and deliver the verdict instead of
  # declaring gate death, or a healthy deploy fails.
  it 'collects a verdict recorded between the read and the status probe' do
    want = CollectDeployVerdict.verdict
    @harness.stage_verdict(want)

    result = @harness.run(cat_failures: CollectDeployVerdict::ONE_FAILURE)

    expect(result.timed_out).to be(false), "collector did not finish:\n#{result.error_output}"
    expect(result.exit_status.exitstatus).to eq(0),
                                            "exit code = #{result.exit_status.exitstatus.inspect}, want 0\n#{result.error_output}"
    expect(JSON.parse(result.output)).to eq(want),
                                         "stdout is not the staged verdict: #{result.output.inspect}"
    expect(result.error_output).to include('Collected deploy verdict'),
                                   "stderr does not confirm collection:\n#{result.error_output}"
  end

  # An inactive unit that never recorded anything is the real gate death, and
  # the deploy must fail loudly rather than wait out its budget.
  it 'confirms gate death when no verdict ever appears' do
    result = @harness.run(cat_failures: CollectDeployVerdict::NEVER_APPEARS)

    expect(result.timed_out).to be(false), "collector did not finish:\n#{result.error_output}"
    expect(result.exit_status.exitstatus).to eq(CollectDeployVerdict::EXIT_GATE_DEATH),
                                            "exit code = #{result.exit_status.exitstatus.inspect}, want 2\n#{result.error_output}"
    expect(result.output).to eq(''), "stdout = #{result.output.inspect}, want empty"
    expect(result.error_output).to include('gate death confirmed'),
                                   "stderr does not confirm gate death:\n#{result.error_output}"
  end

  # A verdict keyed to another run proves nothing about this one, so an inactive
  # unit with only a stale verdict still means this run's gate died without
  # recording.
  it 'rejects a stale verdict after an inactive unit' do
    @harness.stage_verdict(CollectDeployVerdict.verdict(trace_id: 'trace-0-previous-run'))

    result = @harness.run(cat_failures: CollectDeployVerdict::ONE_FAILURE)

    expect(result.timed_out).to be(false), "collector did not finish:\n#{result.error_output}"
    expect(result.exit_status.exitstatus).to eq(CollectDeployVerdict::EXIT_GATE_DEATH),
                                            "exit code = #{result.exit_status.exitstatus.inspect}, want 2\n#{result.error_output}"
    expect(result.output).to eq(''), "stdout = #{result.output.inspect}, want empty"
    expect(result.error_output).to include('Stale-verdict rejection'),
                                   "stderr does not report the stale rejection:\n#{result.error_output}"
    expect(result.error_output).to include('gate death confirmed'),
                                   "stderr does not confirm gate death:\n#{result.error_output}"
  end

  # A re-read that loses connectivity observes nothing about the verdict, so it
  # must not confirm gate death: that would repeat the defect this collector
  # exists to correct, failing a deploy whose gate may well have recorded a
  # passing verdict. The run keeps polling and ends at the timeout instead.
  it 'does not confirm gate death when the re-read loses connectivity' do
    @harness.stage_verdict(CollectDeployVerdict.verdict)

    result = @harness.run(cat_failures: CollectDeployVerdict::ONE_FAILURE,
                          transport_after: CollectDeployVerdict::ONE_FAILURE,
                          budget_seconds: CollectDeployVerdict::TIMEOUT_BUDGET_SECONDS)

    expect(result.timed_out).to be(false), "collector did not finish:\n#{result.error_output}"
    expect(result.exit_status.exitstatus).not_to eq(CollectDeployVerdict::EXIT_GATE_DEATH),
                                                "collector confirmed gate death from a transport failure\n#{result.error_output}"
    expect(result.exit_status.exitstatus).to eq(CollectDeployVerdict::EXIT_TIMED_OUT),
                                            "exit code = #{result.exit_status.exitstatus.inspect}, want 1 (timeout)\n#{result.error_output}"
    expect(result.output).to eq(''), "stdout = #{result.output.inspect}, want empty"
    expect(result.error_output).to include('not confirming gate death'),
                                   "stderr does not report the withheld verdict:\n#{result.error_output}"
    expect(result.error_output).not_to include('gate death confirmed'),
                                       "stderr confirms gate death despite losing the connection:\n#{result.error_output}"
  end

  # Both remote commands run as root, so a quote in an argument must stay data.
  # Interpolating it raw between literal quotes would close the string and run
  # whatever follows. The fake ssh records the command it was handed, so this
  # pins the escaped form rather than the absence of a symptom.
  it 'quotes a verdict path containing a quote' do
    verdict_path = "/run/mwan-deploy-gate/tr'ace.json"
    want_command = %q(cat -- '/run/mwan-deploy-gate/tr'\''ace.json')

    result = @harness.run(verdict_path: verdict_path, cat_failures: CollectDeployVerdict::NEVER_APPEARS)

    expect(result.timed_out).to be(false), "collector did not finish:\n#{result.error_output}"
    expect(@harness.last_remote_read).to eq(want_command),
                                         "remote command = #{@harness.last_remote_read.inspect}, want #{want_command.inspect}"
  end
end
