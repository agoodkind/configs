# frozen_string_literal: true

require 'fileutils'
require 'json'
require 'securerandom'
require_relative 'command_runner'

LEGACY_REF = 'a7754ee3df997fc6249042368d56a8a32405d7a4'
LEGACY_FILES = %w[opensearch-internal-users.yml.j2 opensearch-roles-mapping.yml.j2 opensearch-roles.yml].freeze

abort 'Usage: prepare_search_conversion.rb OUTPUT_DIRECTORY' unless ARGV.length == 1
directory = File.expand_path(ARGV.fetch(0))
repository = File.expand_path('../..', __dir__)
image = 'configs-service-secret-tests'
FileUtils.mkdir_p(File.join(directory, 'desired-source'), mode: 0o700)
FileUtils.mkdir_p(File.join(directory, 'legacy-source'), mode: 0o700)
FileUtils.mkdir_p(File.join(directory, 'legacy-template', 'tack'), mode: 0o700)
build = CommandRunner.run({}, ['docker', 'build', '-t', image, '-f',
                               'spec/fixtures/service_secrets/Dockerfile', '.'], chdir: repository, timeout_seconds: 180)
abort "Build the conversion fixture tools: #{build.output}" unless build.exit_status.success?
generated = CommandRunner.run({}, ['docker', 'run', '--rm', '--network', 'none',
                                   '--mount', "type=bind,src=#{repository},dst=/repo,readonly",
                                   '--mount', "type=bind,src=#{directory},dst=/fixture", image,
                                   '/repo/tack/provision-secrets.sh', '/fixture/desired-source',
                                   '/fixture/absent.env', '/fixture/absent.password'], chdir: repository, timeout_seconds: 60)
abort "Generate the synthetic service credentials: #{generated.output}" unless generated.exit_status.success?
password = "#{SecureRandom.hex(32)}\n"
File.write(File.join(directory, 'OPENSEARCH_LEGACY_PASSWORD'), password, mode: 'w', perm: 0o600)
hashed = CommandRunner.capture(['docker', 'run', '--rm', '-i', '--network', 'none', '--entrypoint', 'htpasswd', image,
                                '-niBC', '12', 'tack_legacy'], stdin_data: password, chdir: repository, timeout_seconds: 30)
abort "Hash the synthetic legacy credential: #{hashed.error_output}" unless hashed.exit_status.success?
File.write(File.join(directory, 'OPENSEARCH_LEGACY_PASSWORD.hash'), hashed.output, mode: 'w', perm: 0o600)
LEGACY_FILES.each do |name|
  source = CommandRunner.capture(['git', 'show', "#{LEGACY_REF}:tack/#{name}"], stdin_data: '', chdir: repository, timeout_seconds: 30)
  abort "Read the immutable legacy policy: #{source.error_output}" unless source.exit_status.success?

  File.write(File.join(directory, 'legacy-template', 'tack', name), source.output, mode: 'w', perm: 0o600)
end
variables = {
  'repository_root' => '/fixture/legacy-template', 'secret_directory' => '/fixture/legacy-source',
  'tack_search_keep_legacy_user' => true, 'tack_search_username' => 'tack_legacy',
  'tack_search_password_hash' => "{{ lookup('file', '/fixture/OPENSEARCH_LEGACY_PASSWORD.hash') | trim | split(':') | last }}"
}
rendered = CommandRunner.run({}, ['docker', 'run', '--rm', '--network', 'none',
                                  '--mount', "type=bind,src=#{repository},dst=/repo,readonly",
                                  '--mount', "type=bind,src=#{directory},dst=/fixture",
                                  '--entrypoint', 'ansible-playbook', image, '--inventory', 'localhost,',
                                  '/repo/spec/fixtures/ansible/render_service_users.yml',
                                  '--extra-vars', JSON.generate(variables)], chdir: repository, timeout_seconds: 60)
abort "Render the immutable legacy security configuration: #{rendered.output}" unless rendered.exit_status.success?
puts 'The synthetic legacy and generated service fixtures are ready.'
