# frozen_string_literal: true

require 'fileutils'
require 'json'
require_relative 'command_runner'

abort 'Usage: apply_search_conversion.rb FIXTURE_DIRECTORY retain|retire' unless ARGV.length == 2
directory = File.expand_path(ARGV.fetch(0))
phase = ARGV.fetch(1)
abort 'The conversion phase must be retain or retire.' unless %w[retain retire].include?(phase)
engine = JSON.parse(File.read(File.join(directory, 'engine.json')))
abort 'The test requires a local Docker container ID.' unless engine.fetch('container').match?(/\A[0-9a-f]{64}\z/)
abort 'The test requires the container-local HTTPS endpoint.' unless engine.fetch('url') == 'https://localhost:9200'
repository = File.expand_path('../..', __dir__)
image = 'configs-service-secret-tests'
build = CommandRunner.run({}, ['docker', 'build', '-t', image, '-f',
                               'spec/fixtures/service_secrets/Dockerfile', '.'],
                          chdir: repository, timeout_seconds: 180)
abort "Build the production task test image: #{build.output}" unless build.exit_status.success?
state = File.join(directory, 'state')
FileUtils.mkdir_p(state, mode: 0o700)
desired = File.join(state, 'desired')
FileUtils.cp_r(File.join(directory, 'desired-source'), desired) unless File.directory?(desired)
variables = {
  'repository_root' => '/repo', 'secret_directory' => '/material/desired',
  'tack_search_keep_legacy_user' => phase == 'retain', 'tack_search_username' => 'tack_legacy',
  'tack_search_password_hash' => "{{ lookup('file', '/fixture/OPENSEARCH_LEGACY_PASSWORD.hash') | trim | split(':') | last }}"
}
result = CommandRunner.run({}, ['docker', 'run', '--rm', '--network', "container:#{engine.fetch('container')}",
                                '--mount', "type=bind,src=#{repository},dst=/repo,readonly",
                                '--mount', "type=bind,src=#{directory},dst=/fixture,readonly",
                                '--mount', "type=bind,src=#{state},dst=/material",
                                '--entrypoint', 'ansible-playbook', image, '--inventory', 'localhost,',
                                '/repo/spec/fixtures/ansible/apply_search_conversion.yml',
                                '--extra-vars', JSON.generate(variables)],
                           chdir: repository, timeout_seconds: 90)
abort "Apply the actual search conversion tasks: #{result.output}" unless result.exit_status.success?
File.write(File.join(directory, "#{phase}.complete"), "complete\n", mode: 'w', perm: 0o600)
puts "The #{phase} conversion tasks passed."
