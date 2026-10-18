# frozen_string_literal: true

require 'digest'
require 'fileutils'
require_relative '../support/command_runner'
require_relative '../support/ledger_authority_fixture'

RSpec.describe 'Tack ledger authority storage' do
  repository = File.expand_path('../..', __dir__)
  image = 'configs-service-secret-tests'

  before(:context) do
    result = CommandRunner.run({}, ['docker', 'build', '-t', image, '-f',
                                    'spec/fixtures/service_secrets/Dockerfile', '.'],
                               chdir: repository, timeout_seconds: 180)
    raise "Build the real authority test image: #{result.output}" unless result.exit_status.success?
  end

  it 'moves a legacy authority outside runtime mounts and preserves every file on later runs', :aggregate_failures do
    LedgerAuthorityFixture.with_material(repository, image, 'tack-ledger-authority') do |directory|
      first = LedgerAuthorityFixture.provision(repository, image, directory)
      expect(first.exit_status.success?).to be(true), first.output
      original = LedgerAuthorityFixture.inspect_material(repository, image, directory)
      fingerprints = LedgerAuthorityFixture.fingerprints(original)
      expect(fingerprints.keys).to include('ca.crt', 'ca.key', 'ca.srl', 'node.yb1.crt', 'node.yb1.csr', 'node.yb1.key')
      staged = LedgerAuthorityFixture.control(repository, image, directory, 'stage-legacy')
      expect(staged.exit_status.success?).to be(true), staged.output
      migrated = LedgerAuthorityFixture.provision(repository, image, directory)
      expect(migrated.exit_status.success?).to be(true), migrated.output
      preserved = LedgerAuthorityFixture.inspect_material(repository, image, directory)
      expect(LedgerAuthorityFixture.path_stat(preserved, '/material/runtime/ca').fetch('exists')).to be(false)
      expect(LedgerAuthorityFixture.fingerprints(preserved)).to eq(fingerprints)
      expect(LedgerAuthorityFixture.path_stat(preserved, '/material')).to include('uid' => 0, 'mode' => '0700')
      expect(LedgerAuthorityFixture.path_stat(preserved, '/material/authority')).to include('uid' => 0, 'mode' => '0700')
      expect(LedgerAuthorityFixture.path_stat(preserved, '/material/authority/ca.key')).to include('uid' => 0, 'mode' => '0600')
      expect(LedgerAuthorityFixture.path_stat(preserved, '/material/runtime/ca.crt').fetch('checksum')).to eq(fingerprints.fetch('ca.crt'))
      repeated = LedgerAuthorityFixture.provision(repository, image, directory)
      expect(repeated.exit_status.success?).to be(true), repeated.output
      expect(LedgerAuthorityFixture.fingerprints(LedgerAuthorityFixture.inspect_material(repository, image, directory))).to eq(fingerprints)
    end
  end

  it 'refuses conflicting authority directories without replacing either source', :aggregate_failures do
    LedgerAuthorityFixture.with_material(repository, image, 'tack-ledger-conflict') do |directory|
      FileUtils.mkdir_p(File.join(directory, 'material', 'authority'))
      FileUtils.mkdir_p(File.join(directory, 'material', 'runtime', 'ca'))
      result = LedgerAuthorityFixture.provision(repository, image, directory)
      expect(result.exit_status.success?).to be(false)
      expect(result.output).to include('Both ledger authority directories exist')
      report = LedgerAuthorityFixture.inspect_material(repository, image, directory)
      expect(LedgerAuthorityFixture.path_stat(report, '/material/runtime/ca').fetch('isdir')).to be(true)
      expect(LedgerAuthorityFixture.path_stat(report, '/material/authority').fetch('isdir')).to be(true)
    end
  end

  it 'refuses an incomplete authority before issuing a replacement', :aggregate_failures do
    LedgerAuthorityFixture.with_material(repository, image, 'tack-ledger-incomplete') do |directory|
      FileUtils.mkdir_p(File.join(directory, 'material', 'authority'))
      File.write(File.join(directory, 'material', 'authority', 'ca.key'), 'synthetic-existing-key', perm: 0o600)
      result = LedgerAuthorityFixture.provision(repository, image, directory)
      expect(result.exit_status.success?).to be(false)
      expect(result.output).to include('The ledger authority key and certificate must both exist')
      report = LedgerAuthorityFixture.inspect_material(repository, image, directory)
      expect(LedgerAuthorityFixture.path_stat(report, '/material/authority/ca.crt').fetch('exists')).to be(false)
      expect(LedgerAuthorityFixture.path_stat(report, '/material/authority/ca.key').fetch('checksum')).to eq(Digest::SHA256.hexdigest('synthetic-existing-key'))
    end
  end
end
