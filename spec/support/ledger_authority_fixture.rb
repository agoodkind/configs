# frozen_string_literal: true

require 'json'
require 'tmpdir'
require_relative 'command_runner'

module LedgerAuthorityFixture
  module_function

  def with_material(repository, image, prefix)
    Dir.mktmpdir(prefix) do |directory|
      %w[material report].each { |name| Dir.mkdir(File.join(directory, name), 0o700) }
      begin
        yield directory
      ensure
        cleanup = control(repository, image, directory, 'cleanup')
        raise "Clean the exact authority fixture: #{cleanup.output}" unless cleanup.exit_status.success?
      end
    end
  end

  def provision(repository, image, directory)
    run(repository, image, directory, 'migrate_ledger_authority.yml', {})
  end

  def control(repository, image, directory, action)
    run(repository, image, directory, 'ledger_authority_material.yml',
        'fixture_action' => action, 'fixture_uid' => Process.uid, 'fixture_gid' => Process.gid)
  end

  def inspect_material(repository, image, directory)
    inspection = control(repository, image, directory, 'inspect')
    raise "Inspect the authority fixture: #{inspection.output}" unless inspection.exit_status.success?

    JSON.parse(File.read(File.join(directory, 'report', 'metadata.json')))
  end

  def fingerprints(report)
    report.fetch('files').to_h { |entry| [entry.fetch('path').delete_prefix('/material/authority/'), entry.fetch('checksum')] }
  end

  def path_stat(report, path)
    report.fetch('paths').find { |entry| entry.fetch('item') == path }.fetch('stat')
  end

  def run(repository, image, directory, playbook, variables)
    CommandRunner.run({}, ['docker', 'run', '--rm', '--network', 'none',
                           '--mount', "type=bind,src=#{repository},dst=/repo,readonly",
                           '--mount', "type=bind,src=#{directory}/material,dst=/material",
                           '--mount', "type=bind,src=#{directory}/report,dst=/report",
                           '--entrypoint', 'ansible-playbook', image, '--inventory', 'localhost,',
                           "/repo/spec/fixtures/ansible/#{playbook}", '--extra-vars', JSON.generate(variables)],
                      chdir: repository, timeout_seconds: 60)
  end
end
