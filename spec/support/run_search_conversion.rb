# frozen_string_literal: true

require 'fileutils'
require 'json'
require 'rbconfig'
require 'securerandom'
require 'tmpdir'
require_relative 'command_runner'

TIMEOUT_SECONDS = 960
POLL_SECONDS = 0.2

def wait_for_file(directory, name, process, deadline)
  path = File.join(directory, name)
  until File.file?(path)
    completed = Process.waitpid2(process, Process::WNOHANG)
    raise "The Tack test exited before #{name}. Read #{directory}/tack-test.log." unless completed.nil?
    raise "The Tack test did not create #{name} within the test deadline." if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

    sleep(POLL_SECONDS)
  end
end

def apply_phase(directory, phase, repository)
  result = CommandRunner.run({}, [RbConfig.ruby, File.join(__dir__, 'apply_search_conversion.rb'), directory, phase],
                             chdir: repository, timeout_seconds: 300)
  File.write(File.join(directory, "#{phase}.log"), result.output, mode: 'w', perm: 0o600)
  raise "The #{phase} conversion failed. Read #{directory}/#{phase}.log." unless result.exit_status.success?
end

def clean_failed_run(directory, runner)
  metadata_path = File.join(directory, 'engine.json')
  containers = [runner]
  if File.file?(metadata_path)
    metadata = JSON.parse(File.read(metadata_path))
    metadata.fetch('containers').each do |identifier|
      raise 'The engine cleanup list contains an invalid container ID.' unless identifier.match?(/\A[0-9a-f]{64}\z/)

      containers.push(identifier)
    end
  end
  result = CommandRunner.run({}, ['docker', 'rm', '--force', '--volumes', *containers], chdir: directory, timeout_seconds: 30)
  warn "Exact test container cleanup returned #{result.exit_status}: #{result.output.strip}" unless result.exit_status.success?
end

abort 'Usage: run_search_conversion.rb TACK_WORKTREE OUTPUT_ROOT' unless ARGV.length == 2
tack = File.expand_path(ARGV.fetch(0))
source = File.expand_path(ARGV.fetch(1))
repository = File.expand_path('../..', __dir__)
runs = File.join(source, 'runs')
FileUtils.mkdir_p(runs, mode: 0o700)
directory = Dir.mktmpdir('conversion-', runs)
prepared = CommandRunner.run({}, [RbConfig.ruby, File.join(__dir__, 'prepare_search_conversion.rb'), directory],
                             chdir: repository, timeout_seconds: 300)
File.write(File.join(directory, 'prepare.log'), prepared.output, mode: 'w', perm: 0o600)
abort "Prepare the synthetic conversion inputs. Read #{directory}/prepare.log." unless prepared.exit_status.success?
runner = "tack-search-conversion-#{SecureRandom.hex(8)}"
environment = { 'TACK_TEST_ROOT' => tack }
build = CommandRunner.run(environment, %w[docker compose -f docker-compose.test.yml --profile runner build tests],
                          chdir: tack, timeout_seconds: 600)
File.write(File.join(directory, 'build.log'), build.output, mode: 'w', perm: 0o600)
abort "Build the Tack test runner. Read #{directory}/build.log." unless build.exit_status.success?
arguments = ['docker', 'compose', '-f', 'docker-compose.test.yml', '--profile', 'runner', 'run', '--rm',
             '--name', runner, '--volume', "#{directory}:#{directory}", '--env', 'TACK_SEARCH_INTEGRATION=1',
             '--env', "TACK_TEST_SEARCH_CONVERSION_DIRECTORY=#{directory}", 'tests', 'test',
             '-tags', 'integration,searchconversion', '-count=1', '-timeout', '15m', '-v',
             '-run', '^TestServiceSecretOpenSearchConversion$', './internal/test/integration/...']
log = File.open(File.join(directory, 'tack-test.log'), 'w', 0o600)
process = Process.spawn(environment, *arguments, chdir: tack, out: log, err: %i[child out])
log.close
success = false
begin
  deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + TIMEOUT_SECONDS
  wait_for_file(directory, 'engine.json', process, deadline)
  apply_phase(directory, 'retain', repository)
  wait_for_file(directory, 'retire.ready', process, deadline)
  apply_phase(directory, 'retire', repository)
  completed = nil
  until completed
    raise 'The Tack conversion test exceeded its deadline.' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

    completed = Process.waitpid2(process, Process::WNOHANG)
    sleep(POLL_SECONDS) unless completed
  end
  raise "The Tack conversion checks failed. Read #{directory}/tack-test.log." unless completed.last.success?

  success = true
  puts "The real search conversion passed. Evidence: #{directory}"
rescue StandardError => e
  File.write(File.join(directory, 'conversion.failed'), "#{e.message}\n", mode: 'w', perm: 0o600)
  warn e.message
ensure
  clean_failed_run(directory, runner) unless success
end
exit(success ? 0 : 1)
