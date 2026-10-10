# frozen_string_literal: true

require 'json'
require 'securerandom'
require 'tmpdir'
require 'yaml'
require_relative 'command_runner'

module TackSearchOutageCompose
  DOCKER_COMMAND = 'docker'
  IMAGE = 'alpine:3.22'
  SERVICE = 'opensearch'
  PROJECT_PREFIX = 'outage-drill'
  PROJECT_SUFFIX_BYTES = 4
  COMPOSE_FILE = 'compose.yaml'
  # Docker sends signal 9 after the one-second grace period.
  SERVICE_COMMAND = ['sh', '-c', 'trap "" TERM; while true; do sleep 1; done'].freeze
  STOP_GRACE_PERIOD = '1s'
  COMMAND_TIMEOUT_SECONDS = 120

  module_function

  def with_project
    Dir.mktmpdir('search-outage-compose') do |directory|
      project = "#{PROJECT_PREFIX}-#{SecureRandom.hex(PROJECT_SUFFIX_BYTES)}"
      File.write(File.join(directory, COMPOSE_FILE), YAML.dump(definition(project)))
      begin
        docker(directory, %w[compose up --detach --wait])
        yield directory, "#{project}-#{SERVICE}-1"
      ensure
        docker(directory, %w[compose down --timeout 0])
      end
    end
  end

  def definition(project)
    service = { 'image' => IMAGE, 'command' => SERVICE_COMMAND, 'stop_grace_period' => STOP_GRACE_PERIOD }
    { 'name' => project, 'services' => { SERVICE => service } }
  end

  def running?(directory, container)
    JSON.parse(docker(directory, ['container', 'inspect', container])).first.fetch('State').fetch('Running')
  end

  def docker(directory, arguments)
    argv = [DOCKER_COMMAND, *arguments]
    result = CommandRunner.capture(argv, stdin_data: '', chdir: directory, timeout_seconds: COMMAND_TIMEOUT_SECONDS)
    raise "Docker command timed out: #{argv.join(' ')}\n#{result.error_output}" if result.timed_out
    raise "Docker command failed: #{argv.join(' ')}: #{result.exit_status}\n#{result.error_output}" unless result.exit_status.success?

    result.output
  end
end
