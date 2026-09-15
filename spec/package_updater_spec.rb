# frozen_string_literal: true

require 'fileutils'
require 'open3'
require 'tmpdir'

# The weekly package updater runs from a systemd timer on hypervisors and
# guests. These checks run the real script under bash with fake apt-get and
# dpkg first on PATH.
module PackageUpdater
  REPOSITORY_ROOT = File.expand_path('..', __dir__)
  SCRIPT = File.join(REPOSITORY_ROOT, 'common', 'scripts', 'package-updater.sh')
  FIXTURE_DIRECTORY = File.join(REPOSITORY_ROOT, 'spec', 'fixtures', 'package_updater')
  RECORD_ENV = 'PACKAGE_UPDATER_RECORD'
  UPDATE_GATE_ENV = 'FAKE_APT_GET_UPDATE_GATE'
  DPKG_EXIT_CODE_ENV = 'FAKE_DPKG_EXIT_CODE'

  CONFIGURE_CALL = 'dpkg --force-confold --configure -a'
  UPDATE_CALL = 'apt-get update'
  UPDATE_DONE = 'apt-get update finished'
  FULL_UPGRADE_CALL = 'apt-get -o Dpkg::Options::=--force-confold full-upgrade -y'
  AUTOREMOVE_CALL = 'apt-get autoremove -y'
  AUTOCLEAN_CALL = 'apt-get autoclean'

  STOPPED_EXIT_CODE = 143
  WAIT_TIMEOUT_SECONDS = 10
  POLL_INTERVAL_SECONDS = 0.02
  FAKE_MODE = 0o700
  GATE_MODE = 0o600

  # One run's fake PATH, call record, update gate, and output file.
  class Harness
    attr_reader :output_path

    def initialize(work_directory, dpkg_exit_code)
      fake_bin = File.join(work_directory, 'bin')
      FileUtils.mkdir_p(fake_bin)
      install_fake('fake-apt-get.sh', File.join(fake_bin, 'apt-get'))
      install_fake('fake-dpkg.sh', File.join(fake_bin, 'dpkg'))
      @record = File.join(work_directory, 'record')
      @gate = File.join(work_directory, 'update-gate')
      @output_path = File.join(work_directory, 'output')
      @environment = {
        'PATH' => "#{fake_bin}#{File::PATH_SEPARATOR}#{ENV.fetch('PATH')}",
        RECORD_ENV => @record,
        UPDATE_GATE_ENV => @gate,
        DPKG_EXIT_CODE_ENV => dpkg_exit_code.to_s
      }
    end

    # Runs the script to completion and returns its joined output and status.
    def run
      Open3.capture2e(@environment, 'bash', SCRIPT)
    end

    # Starts the script with its joined output in output_path.
    def start
      Process.spawn(@environment, 'bash', SCRIPT, %i[out err] => [output_path, 'w'])
    end

    def output
      File.read(output_path)
    end

    def open_gate
      File.write(@gate, '', perm: GATE_MODE)
    end

    def calls
      return [] unless File.exist?(@record)

      File.read(@record).sub(/\n+\z/, '').split("\n")
    end

    # Waits until the record shows call, and reports whether it did before the
    # deadline.
    def call_recorded?(call)
      deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + WAIT_TIMEOUT_SECONDS
      until calls.include?(call)
        return false if Process.clock_gettime(Process::CLOCK_MONOTONIC) > deadline

        sleep POLL_INTERVAL_SECONDS
      end
      true
    end

    private

    def install_fake(name, target)
      FileUtils.cp(File.join(FIXTURE_DIRECTORY, name), target)
      File.chmod(FAKE_MODE, target)
    end
  end
end

RSpec.describe PackageUpdater do
  around do |example|
    Dir.mktmpdir('package-updater') do |work_directory|
      @work_directory = work_directory
      example.run
    end
  end

  # Pins the healing step: a run that an earlier stop or timeout interrupted
  # mid-configure leaves packages half configured, and apt-get refuses to
  # upgrade until dpkg configures them.
  it 'configures pending packages before upgrading' do
    harness = PackageUpdater::Harness.new(@work_directory, 0)
    harness.open_gate

    output, status = harness.run
    want = [
      PackageUpdater::CONFIGURE_CALL, PackageUpdater::UPDATE_CALL, PackageUpdater::UPDATE_DONE,
      PackageUpdater::FULL_UPGRADE_CALL, PackageUpdater::AUTOREMOVE_CALL, PackageUpdater::AUTOCLEAN_CALL
    ]

    expect(status.exitstatus).to eq(0), "exit code = #{status.exitstatus.inspect}, want 0\n#{output}"
    expect(harness.calls).to eq(want), "calls = #{harness.calls.inspect}, want #{want.inspect}\n#{output}"
    expect(output).to include('dpkg --configure -a exit_code=0'),
                      "output does not log the configure exit code:\n#{output}"
  end

  # Pins that a configure failure is logged with its exit code and ends the run
  # before apt-get touches the package set, and that the unit sees the same
  # exit code.
  it 'stops when configuring pending packages fails' do
    dpkg_failure = 2
    harness = PackageUpdater::Harness.new(@work_directory, dpkg_failure)
    harness.open_gate

    output, status = harness.run
    want = [PackageUpdater::CONFIGURE_CALL]

    expect(status.exitstatus).to eq(dpkg_failure),
                                 "exit code = #{status.exitstatus.inspect}, want #{dpkg_failure}\n#{output}"
    expect(harness.calls).to eq(want), "calls = #{harness.calls.inspect}, want #{want.inspect}\n#{output}"
    expect(output).to include("failed exit_code=#{dpkg_failure}"),
                      "output does not log the configure failure:\n#{output}"
  end

  # Signals only the script, as the unit's KillMode=process does on stop, while
  # apt-get is running. The running step must complete and the script must exit
  # before starting the next one, so a stop never cuts dpkg off mid-configure.
  it 'finishes the running step when stopped' do
    harness = PackageUpdater::Harness.new(@work_directory, 0)
    process_id = harness.start

    unless harness.call_recorded?(PackageUpdater::UPDATE_CALL)
      Process.kill('KILL', process_id)
      Process.wait(process_id)
      raise "apt-get update never started\n#{harness.output}"
    end

    Process.kill('TERM', process_id)
    harness.open_gate
    _, status = Process.wait2(process_id)
    output = harness.output
    want = [PackageUpdater::CONFIGURE_CALL, PackageUpdater::UPDATE_CALL, PackageUpdater::UPDATE_DONE]

    expect(harness.calls).to eq(want), "calls = #{harness.calls.inspect}, want #{want.inspect}\n#{output}"
    expect(status.exitstatus).to eq(PackageUpdater::STOPPED_EXIT_CODE),
                                 "exit code = #{status.exitstatus.inspect}, want #{PackageUpdater::STOPPED_EXIT_CODE}\n#{output}"
    expect(output).to include("stopped before: #{PackageUpdater::FULL_UPGRADE_CALL}"),
                      "output does not log where the run stopped:\n#{output}"
  end
end
