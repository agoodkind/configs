# frozen_string_literal: true

require 'digest'
require 'securerandom'
require 'tmpdir'
require_relative '../support/ansible_render'

RSpec.describe 'Tack connection file rendering' do
  repository = AnsibleRender::REPOSITORY_ROOT
  addresses = { 'yb1' => '2001:db8::21', 'yb2' => '2001:db8::22', 'yb3' => '2001:db8::23' }.freeze

  before(:context) do
    result = CommandRunner.run({}, ['docker', 'build', '-t', 'configs-service-secret-tests', '-f',
                                    'spec/fixtures/service_secrets/Dockerfile', '.'],
                               chdir: repository, timeout_seconds: 180)
    raise "Build the real credential provisioner image: #{result.output}" unless result.exit_status.success?
  end

  def generate_credentials(repository, directory)
    CommandRunner.run({}, ['docker', 'run', '--rm', '--user', "#{Process.uid}:#{Process.gid}",
                           '--mount', "type=bind,src=#{repository},dst=/repo,readonly",
                           '--mount', "type=bind,src=#{directory},dst=/secrets",
                           'configs-service-secret-tests', '/repo/tack/provision-secrets.sh',
                           '/secrets', '/secrets/previous.env', '/secrets/app-db.password'],
                      chdir: repository, timeout_seconds: 60)
  end

  def render_connections(repository, directory)
    CommandRunner.run({}, ['docker', 'run', '--rm', '--user', "#{Process.uid}:#{Process.gid}",
                           '--mount', "type=bind,src=#{directory},dst=/secrets",
                           'configs-service-secret-tests', '/secrets/render-connections.sh'],
                      chdir: repository, timeout_seconds: 60)
  end

  def adopt_awkward_credentials(directory)
    names = %w[YUGABYTE_PASSWORD AUDIT_WRITER_PASSWORD AUDIT_READER_PASSWORD
               AUDIT_REDACTOR_PASSWORD AUDIT_OPERATOR_PASSWORD TACK_MIGRATOR_PASSWORD]
    passwords = names.to_h { |name| [name, "#{awkward_password}\n"] }
    previous = passwords.map { |name, password| "#{name}=#{password}" }.join
    File.write(File.join(directory, 'previous.env'), previous, perm: 0o600)
    passwords['TACK_APP_PASSWORD'] = awkward_password
    passwords['CLICKHOUSE_PASSWORD'] = "#{awkward_password}\n"
    File.write(File.join(directory, 'app-db.password'), passwords.fetch('TACK_APP_PASSWORD'), perm: 0o600)
    File.write(File.join(directory, 'CLICKHOUSE_PASSWORD'), passwords.fetch('CLICKHOUSE_PASSWORD'), perm: 0o600)
    passwords.transform_values { |password| Digest::SHA256.hexdigest(password) }
  end

  def awkward_password
    " \t#{SecureRandom.hex(16)}#{(' '..'~').to_a.shuffle.join}é\t\\' "
  end

  def check_connections(directory, phase)
    environment = ENV.keys.grep(/\APG/).to_h { |name| [name, nil] }
    passfile = File.join(directory, 'pgpass')
    File.write(passfile, '', perm: 0o600)
    environment['PGPASSFILE'] = passfile
    CommandRunner.run(environment, ['go', 'run', '.', directory, phase],
                      chdir: File.join(AnsibleRender::REPOSITORY_ROOT, 'spec/fixtures/service_secrets'),
                      timeout_seconds: 120)
  end

  def expect_protected_files(directory)
    Dir.glob(File.join(directory, '*')).each do |path|
      metadata = File.stat(path)
      expect(metadata.mode & 0o777).to eq(0o600) unless File.basename(path) == 'render-connections.sh'
      expect([metadata.uid, metadata.gid]).to eq([Process.uid, Process.gid])
    end
  end

  [[true, true], [false, true], [true, false]].each do |repointed, adopted|
    it "preserves passwords and connection settings with repointed=#{repointed} and adopted=#{adopted}", :aggregate_failures do
      Dir.mktmpdir('tack-connection-files') do |directory|
        expected = {}
        expected = adopt_awkward_credentials(directory) if adopted
        generation = generate_credentials(repository, directory)
        expect(generation.exit_status.success?).to be(true), generation.output
        expected.each do |name, fingerprint|
          expect(Digest::SHA256.file(File.join(directory, name)).hexdigest).to eq(fingerprint)
        end
        AnsibleRender.render(
          inventory: 'localhost,', playbook: 'render_tack_connections.yml',
          extra_vars: {
            'repository_root' => repository, 'output_directory' => directory,
            'group_vars_file' => File.join(repository, 'ansible/inventory/group_vars/tack_all.yml'),
            'tack_secrets_dir' => '/secrets', 'tack_store_host' => '2001:db8::20',
            'tack_ledger_node_addresses' => addresses, 'tack_ledger_consumers_repointed' => repointed,
            'tack_ledger_tls_enabled' => false,
            'tack_provision_owner' => true, 'tack_app_guest' => false, 'tack_ledger_bootstrap' => false,
            'tack_ops_migrator_login' => true
          }
        )
        rendering = render_connections(repository, directory)
        expect(rendering.exit_status.success?).to be(true), rendering.output
        expect(rendering.output).to be_empty
        phase = 'legacy'
        phase = 'repointed' if repointed
        parsing = check_connections(directory, phase)
        expect(parsing.exit_status.success?).to be(true), parsing.output
        expect(parsing.output).to be_empty
        expect_protected_files(directory)
      end
    end
  end
end
