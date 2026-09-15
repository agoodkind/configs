# frozen_string_literal: true

require 'json'
require 'tmpdir'
require_relative 'command_runner'

# Renders a fixture play from the ansible directory, so the play loads the
# repository's Ansible configuration the way a deploy does.
module AnsibleRender
  REPOSITORY_ROOT = File.expand_path('../..', __dir__)
  ANSIBLE_DIRECTORY = File.join(REPOSITORY_ROOT, 'ansible')
  FIXTURE_DIRECTORY = File.join(REPOSITORY_ROOT, 'spec', 'fixtures', 'ansible')
  PLAYBOOK_COMMAND = 'ansible-playbook'
  PLAYBOOK_TIMEOUT_SECONDS = 30
  VAULT_PASSWORD_ENV = 'ANSIBLE_VAULT_PASSWORD_FILE'
  VAULT_PASSWORD_PLACEHOLDER = "unused\n"
  SECRET_FILE_MODE = 0o600

  module_function

  def render(inventory:, playbook:, extra_vars:)
    Dir.mktmpdir('vault-password') do |password_directory|
      password_file = File.join(password_directory, 'vault-password')
      File.write(password_file, VAULT_PASSWORD_PLACEHOLDER, perm: SECRET_FILE_MODE)
      result = run_playbook(password_file, inventory, playbook, extra_vars)
      raise "render #{playbook} exceeded #{PLAYBOOK_TIMEOUT_SECONDS}s\n#{result.output}" if result.timed_out
      raise "render #{playbook}: #{result.exit_status}\n#{result.output}" unless result.exit_status.success?
    end
  end

  def run_playbook(password_file, inventory, playbook, extra_vars)
    argv = [
      PLAYBOOK_COMMAND,
      '--inventory', inventory,
      File.join(FIXTURE_DIRECTORY, playbook),
      '--extra-vars', JSON.generate(extra_vars)
    ]
    CommandRunner.run(
      { VAULT_PASSWORD_ENV => password_file },
      argv,
      chdir: ANSIBLE_DIRECTORY,
      timeout_seconds: PLAYBOOK_TIMEOUT_SECONDS
    )
  rescue Errno::ENOENT => e
    raise "#{PLAYBOOK_COMMAND} is required: #{e.message}"
  end
end
