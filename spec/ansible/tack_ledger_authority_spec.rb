# frozen_string_literal: true

require 'digest'
require 'fileutils'
require 'tmpdir'
require_relative '../support/command_runner'

RSpec.describe 'Tack ledger authority storage' do
  repository = File.expand_path('../..', __dir__)
  image = 'configs-service-secret-tests'

  before(:context) do
    result = CommandRunner.run({}, ['docker', 'build', '-t', image, '-f',
                                    'spec/fixtures/service_secrets/Dockerfile', '.'],
                               chdir: repository, timeout_seconds: 180)
    raise "Build the real authority test image: #{result.output}" unless result.exit_status.success?
  end

  def provision_authority(repository, image, directory)
    CommandRunner.run({}, ['docker', 'run', '--rm', '--network', 'none',
                           '--mount', "type=bind,src=#{repository},dst=/repo,readonly",
                           '--mount', "type=bind,src=#{directory},dst=/material",
                           '--entrypoint', 'ansible-playbook', image, '--inventory', 'localhost,',
                           '/repo/spec/fixtures/ansible/migrate_ledger_authority.yml'],
                      chdir: repository, timeout_seconds: 60)
  end

  def authority_fingerprints(directory)
    Dir.children(directory).to_h { |name| [name, Digest::SHA256.file(File.join(directory, name)).hexdigest] }
  end

  it 'moves a legacy authority outside runtime mounts and preserves every file on later runs', :aggregate_failures do
    Dir.mktmpdir('tack-ledger-authority') do |directory|
      first = provision_authority(repository, image, directory)
      expect(first.exit_status.success?).to be(true), first.output
      authority = File.join(directory, 'authority')
      fingerprints = authority_fingerprints(authority)
      legacy = File.join(directory, 'runtime', 'ca')
      FileUtils.mv(authority, legacy)
      migrated = provision_authority(repository, image, directory)
      expect(migrated.exit_status.success?).to be(true), migrated.output
      expect(File.exist?(legacy)).to be(false)
      expect(authority_fingerprints(authority)).to eq(fingerprints)
      expect(File.stat(authority).mode & 0o777).to eq(0o700)
      expect(Digest::SHA256.file(File.join(directory, 'runtime', 'ca.crt')).hexdigest).to eq(fingerprints.fetch('ca.crt'))
      repeated = provision_authority(repository, image, directory)
      expect(repeated.exit_status.success?).to be(true), repeated.output
      expect(authority_fingerprints(authority)).to eq(fingerprints)
    end
  end

  it 'refuses conflicting authority directories without replacing either source', :aggregate_failures do
    Dir.mktmpdir('tack-ledger-conflict') do |directory|
      FileUtils.mkdir_p(File.join(directory, 'authority'))
      FileUtils.mkdir_p(File.join(directory, 'runtime', 'ca'))
      result = provision_authority(repository, image, directory)
      expect(result.exit_status.success?).to be(false)
      expect(result.output).to include('Both ledger authority directories exist')
      expect(File.directory?(File.join(directory, 'runtime', 'ca'))).to be(true)
    end
  end

  it 'refuses an incomplete authority before issuing a replacement', :aggregate_failures do
    Dir.mktmpdir('tack-ledger-incomplete') do |directory|
      FileUtils.mkdir_p(File.join(directory, 'authority'))
      File.write(File.join(directory, 'authority', 'ca.key'), 'synthetic-existing-key', perm: 0o600)
      result = provision_authority(repository, image, directory)
      expect(result.exit_status.success?).to be(false)
      expect(result.output).to include('The ledger authority key and certificate must both exist')
      expect(File.exist?(File.join(directory, 'authority', 'ca.crt'))).to be(false)
    end
  end
end
