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
  SHEBANG_PREFIX = '#!'

  module_function

  # The interpreter that runs ansible-core, read from the ansible-playbook entry
  # point's shebang, so a script imports the same ansible-core a deploy runs.
  # The entry point is only read, never run.
  def ansible_python
    entry_point = executable_on_path(PLAYBOOK_COMMAND)
    raise "#{PLAYBOOK_COMMAND} is required: not found on PATH" if entry_point.nil?

    first_line = File.open(entry_point, &:gets).to_s
    raise "#{entry_point} has no shebang: #{first_line.inspect}" unless first_line.start_with?(SHEBANG_PREFIX)

    fields = first_line.strip.delete_prefix(SHEBANG_PREFIX).split
    raise "#{entry_point} has no shebang: #{first_line.inspect}" if fields.empty?
    return fields.last if File.basename(fields.first) == 'env' && fields.size > 1

    fields.first
  end

  def executable_on_path(command_name)
    candidates = ENV.fetch('PATH').split(File::PATH_SEPARATOR).map { |directory| File.join(directory, command_name) }
    candidates.find { |candidate| File.file?(candidate) && File.executable?(candidate) }
  end

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
