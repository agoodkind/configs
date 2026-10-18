# frozen_string_literal: true

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

  it 'keeps credentials in files and retains database failover and connection bounds', :aggregate_failures do
    Dir.mktmpdir('tack-connection-files') do |directory|
      generation = generate_credentials(repository, directory)
      expect(generation.exit_status.success?).to be(true), generation.output
      File.write(File.join(directory, 'TACK_APP_PASSWORD'), File.read(File.join(directory, 'TACK_APP_PASSWORD')).strip)
      AnsibleRender.render(
        inventory: 'localhost,', playbook: 'render_tack_connections.yml',
        extra_vars: {
          'repository_root' => repository, 'output_directory' => directory,
          'group_vars_file' => File.join(repository, 'ansible/inventory/group_vars/tack_all.yml'),
          'tack_secrets_dir' => '/secrets', 'tack_store_host' => '2001:db8::20',
          'tack_ledger_node_addresses' => addresses, 'tack_ledger_consumers_repointed' => true,
          'tack_provision_owner' => true, 'tack_app_guest' => false, 'tack_ledger_bootstrap' => false,
          'tack_ops_migrator_login' => true
        }
      )
      rendering = render_connections(repository, directory)
      expect(rendering.exit_status.success?).to be(true), rendering.output
      expect(rendering.output).to be_empty
      app = File.read(File.join(directory, 'DATABASE_URL'))
      operator = File.read(File.join(directory, 'TACK_OPS_DATABASE_URL'))
      expect(app.include?('@yb1:5433,yb2:5433,yb3:5433/tack?')).to be(true)
      expect(app.include?('connect_timeout=1')).to be(true)
      expect(operator.include?('host=2001:db8::21,2001:db8::22,2001:db8::23')).to be(true)
      expect(operator.include?('user=tack_migrator')).to be(true)
      expect(operator.include?('connect_timeout=1')).to be(true)
      clickhouse = File.read(File.join(directory, 'AUDIT_CONSUMER_CLICKHOUSE_DSN'))
      expect(clickhouse.start_with?('clickhouse://tack_audit_consumer:')).to be(true)
      expect(clickhouse.end_with?("@clickhouse:9000/audit\n")).to be(true)
      expect(File.stat(File.join(directory, 'DATABASE_URL')).mode & 0o777).to eq(0o600)
    end
  end
end
