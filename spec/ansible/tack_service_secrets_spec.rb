# frozen_string_literal: true

require 'digest'
require 'securerandom'
require 'tmpdir'
require_relative '../support/command_runner'

RSpec.describe 'Tack service credential provisioning' do
  repository = File.expand_path('../..', __dir__)
  image = 'configs-service-secret-tests'
  password_names = %w[
    YUGABYTE_PASSWORD AUDIT_WRITER_PASSWORD AUDIT_READER_PASSWORD
    AUDIT_REDACTOR_PASSWORD AUDIT_OPERATOR_PASSWORD TACK_APP_PASSWORD
    TACK_MIGRATOR_PASSWORD OPENSEARCH_PASSWORD OPENSEARCH_MAINTENANCE_PASSWORD
    OPENSEARCH_HEALTH_PASSWORD CLICKHOUSE_PASSWORD
  ].freeze

  before(:context) do
    result = CommandRunner.run({}, ['docker', 'build', '-t', image, '-f',
                                    'spec/fixtures/service_secrets/Dockerfile', '.'],
                               chdir: repository, timeout_seconds: 180)
    raise "Build the real credential provisioner image: #{result.output}" unless result.exit_status.success?
  end

  def provision(repository, image, directory)
    CommandRunner.run({}, ['docker', 'run', '--rm',
                           '--mount', "type=bind,src=#{repository},dst=/repo,readonly",
                           '--mount', "type=bind,src=#{directory},dst=/secrets", image,
                           '/repo/tack/provision-secrets.sh', '/secrets',
                           '/secrets/previous.env', '/secrets/app-db.password'],
                      chdir: repository, timeout_seconds: 60)
  end

  def fingerprint(path)
    Digest::SHA256.file(path).hexdigest
  end

  it 'generates distinct protected credentials and preserves them on another provision', :aggregate_failures do
    Dir.mktmpdir('tack-service-secrets') do |directory|
      first = provision(repository, image, directory)
      expect(first.exit_status.success?).to be(true), first.output
      expect(first.output.strip).to eq('changed')
      passwords = password_names.map { |name| File.join(directory, name) }
      passwords.each do |path|
        expect(File.read(path).strip.match?(/\A[0-9a-f]{64}\z/)).to be(true), File.basename(path)
        expect(File.stat(path).mode & 0o777).to eq(0o600)
      end
      names = password_names + %w[OPENSEARCH_PASSWORD.hash OPENSEARCH_MAINTENANCE_PASSWORD.hash OPENSEARCH_HEALTH_PASSWORD.hash OPENSEARCH_CURSOR_KEY]
      protected_files = names.map { |name| File.join(directory, name) }
      before = protected_files.to_h { |path| [path, fingerprint(path)] }
      expect(before.values.uniq.length).to eq(names.length)
      second = provision(repository, image, directory)
      expect(second.exit_status.success?).to be(true), second.output
      expect(second.output).to be_empty
      expect(protected_files.to_h { |path| [path, fingerprint(path)] }).to eq(before)
    end
  end

  it 'adopts existing SQL credentials and creates separate search identities', :aggregate_failures do
    Dir.mktmpdir('tack-secret-adoption') do |directory|
      previous = SecureRandom.hex(32)
      File.write(File.join(directory, 'previous.env'),
                 "AUDIT_OPERATOR_PASSWORD=#{previous}\nOPENSEARCH_PASSWORD=#{previous}\n", perm: 0o600)
      File.write(File.join(directory, 'app-db.password'), "#{previous}\n", perm: 0o600)
      result = provision(repository, image, directory)
      expect(result.exit_status.success?).to be(true), result.output
      expected = Digest::SHA256.hexdigest("#{previous}\n")
      expect(fingerprint(File.join(directory, 'AUDIT_OPERATOR_PASSWORD'))).to eq(expected)
      expect(fingerprint(File.join(directory, 'TACK_APP_PASSWORD'))).to eq(expected)
      expect(fingerprint(File.join(directory, 'OPENSEARCH_PASSWORD'))).not_to eq(expected)
      expect(fingerprint(File.join(directory, 'OPENSEARCH_MAINTENANCE_PASSWORD'))).not_to eq(expected)
      expect(result.output).not_to include(previous)
    end
  end

  it 'refuses an existing empty credential instead of rotating it', :aggregate_failures do
    Dir.mktmpdir('tack-empty-secret') do |directory|
      File.write(File.join(directory, 'TACK_APP_PASSWORD'), '', perm: 0o600)
      result = provision(repository, image, directory)
      expect(result.exit_status.success?).to be(false)
      expect(result.output).to include('TACK_APP_PASSWORD is empty')
      expect(File.size(File.join(directory, 'TACK_APP_PASSWORD'))).to eq(0)
    end
  end
end
