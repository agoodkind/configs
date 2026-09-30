# frozen_string_literal: true

require 'json'
require 'tmpdir'
require_relative '../support/ansible_render'

RSpec.describe 'MWAN role activation with actual systemd' do
  before do
    @container = ENV.fetch('MWAN_ROLE_ACTIVATION_TEST_CONTAINER', nil)
    skip 'Set MWAN_ROLE_ACTIVATION_TEST_CONTAINER to the isolated systemd fixture' unless @container
  end

  def guest(*arguments)
    result = CommandRunner.run({}, ['docker', 'exec', @container, *arguments],
                               chdir: AnsibleRender::REPOSITORY_ROOT, timeout_seconds: 30)
    raise "guest command failed: #{result.output}" unless result.exit_status.success? && !result.timed_out

    result.output
  end

  def activation_play(directory, phase, prior, current, options)
    inventory = File.join(directory, 'inventory.ini')
    File.write(inventory, "[role_tests]\n#{@container}\n")
    AnsibleRender.render(
      inventory: inventory, playbook: 'activate_mwan_roles.yml', timeout_seconds: 120,
      extra_vars: {
        'repository_root' => AnsibleRender::REPOSITORY_ROOT,
        'role_test_host' => @container, 'role_test_phase' => phase,
        'role_test_prior_network' => prior, 'role_test_current_network' => current,
        'role_test_prepared' => options.fetch(:prepared), 'role_test_corrupt_unit' => options.fetch(:corrupt),
        'mwan_dns_domain' => options.fetch(:domain)
      }
    )
  rescue RuntimeError => e
    File.write(File.join(directory, "#{phase}.log"), e.message)
    evidence_directory = ENV.fetch('MWAN_ROLE_ACTIVATION_TEST_EVIDENCE', nil)
    if evidence_directory
      evidence_path = File.join(evidence_directory, "#{File.basename(directory)}-#{phase}.log")
      File.write(evidence_path, e.message)
    end
    raise
  end

  def role_state(name)
    JSON.parse(guest('networkctl', 'status', name, '--json=short'))
  end

  def with_real_network(legacy_transit: false)
    Dir.mktmpdir('mwan-role-activation') do |directory|
      current = File.join(directory, 'current.json')
      prior = File.join(directory, 'prior.json')
      AnsibleRender.render(inventory: 'localhost,', playbook: 'render_mwan_network.yml',
                           extra_vars: { 'repository_root' => AnsibleRender::REPOSITORY_ROOT,
                                         'network_output' => current })
      document = JSON.parse(File.read(current))
      if legacy_transit
        document.fetch('ietf-interfaces:interfaces').fetch('interface').each do |entry|
          next unless %w[enmgmt0 enmwanbr0].include?(entry.fetch('name'))

          entry['goodkind-mwan-steering:link-files'] = 'hand-authored'
          entry.delete('goodkind-mwan-steering:link')
          entry.delete('ietf-ip:ipv4')
          entry.delete('ietf-ip:ipv6')
        end
      end
      File.write(prior, legacy_transit ? JSON.generate(document) : File.read(current))
      prepared = guest('systemctl', 'show', 'mwan-ifmgr@wan', '--property=LoadState', '--value').strip == 'loaded'
      activation_play(directory, 'prepare', prior, current, prepared: prepared, corrupt: false,
                                                            domain: 'home.goodkind.io')
      yield directory, prior, current
    end
  end

  it 'verifies generated units before retiring legacy role files' do
    with_real_network do |directory, prior, current|
      preserved = guest('find', '/etc/systemd/network', '-type', 'f').lines.map(&:strip).reject do |path|
        %w[10-mgmt.network 40-mwanbr.network 20-enmgmt0.network 20-enmwanbr0.network].include?(File.basename(path))
      end.sort
      prior_files = guest('sha256sum', *preserved)
      activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                             domain: 'home.goodkind.io')
      expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/20-enmgmt0.network')
      expect(role_state('enmwanbr0').fetch('NetworkFile')).to eq('/etc/systemd/network/20-enmwanbr0.network')
      expect(guest('find', '/etc/systemd/network', '-name', '10-mgmt.network', '-o', '-name', '40-mwanbr.network')).to eq('')
      expect(guest('sha256sum', *preserved)).to eq(prior_files)
    end
  end

  it 'preserves legacy inputs when the actual generated-unit verifier rejects a changed unit' do
    with_real_network do |directory, prior, current|
      legacy = guest('cat', '/etc/systemd/network/10-mgmt.network')
      expect do
        activation_play(directory, 'activate', prior, current, prepared: true, corrupt: true,
                                                               domain: 'home.goodkind.io')
      end.to raise_error(RuntimeError, /Verify the installed generated networkd units: .*non-zero return code/)
      expect(guest('cat', '/etc/systemd/network/10-mgmt.network')).to eq(legacy)
      expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/10-mgmt.network')
    end
  end

  [false, true].each do |legacy_transit|
    it "restores observed role selection after activation fails with legacy_transit=#{legacy_transit}" do
      with_real_network(legacy_transit: legacy_transit) do |directory, prior, current|
        before = %w[enmgmt0 enmwanbr0].map { |name| role_state(name).fetch('NetworkFile') }
        legacy = guest('cat', '/etc/systemd/network/10-mgmt.network')
        expect do
          activation_play(directory, 'activate', prior, current, prepared: true,
                                                                 corrupt: false, domain: 'wrong.test')
        end.to raise_error(RuntimeError, /prior role inputs and applied state passed recovery verification/)
        expect(%w[enmgmt0 enmwanbr0].map { |name| role_state(name).fetch('NetworkFile') }).to eq(before)
        expect(guest('cat', '/etc/systemd/network/10-mgmt.network')).to eq(legacy)
        expect(JSON.parse(guest('cat', '/etc/mwan/network.json'))).to eq(JSON.parse(File.read(prior)))
      end
    end
  end

  %w[prefix metric].each do |field|
    it "rejects and recovers an applied role #{field} that differs from inventory" do
      with_real_network do |directory, prior, current|
        document = JSON.parse(File.read(current))
        interfaces = document.fetch('ietf-interfaces:interfaces').fetch('interface')
        if field == 'prefix'
          management = interfaces.find { |entry| entry.fetch('name') == 'enmgmt0' }
          management.fetch('ietf-ip:ipv6').fetch('address').first['prefix-length'] = 63
        else
          transit = interfaces.find { |entry| entry.fetch('name') == 'enmwanbr0' }
          transit.fetch('ietf-ip:ipv6').fetch('goodkind-mwan-steering:route').first['metric'] = 42
        end
        File.write(current, JSON.generate(document))
        expect do
          activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                                 domain: 'home.goodkind.io')
        end.to raise_error(RuntimeError, /prior role inputs and applied state passed recovery verification/)
        expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/10-mgmt.network')
        expect(JSON.parse(guest('cat', '/etc/mwan/network.json'))).to eq(JSON.parse(File.read(prior)))
      end
    end
  end

  def remove_initial_role_configuration
    guest('systemctl', 'disable', '--now', 'mwan-ifmgr@wan')
    guest('rm', '-f', '/etc/mwan/config.toml', '/etc/mwan/network.json',
          '/etc/systemd/network/10-mgmt.network', '/etc/systemd/network/40-mwanbr.network',
          '/etc/systemd/network/20-enmgmt0.network', '/etc/systemd/network/20-enmwanbr0.network',
          '/etc/systemd/network/10-mgmt.link', '/etc/systemd/network/40-mwanbr.link',
          '/etc/systemd/network/20-enmgmt0.link', '/etc/systemd/network/20-enmwanbr0.link')
    %w[enmgmt0 enmwanbr0].each_with_index do |name, index|
      guest('ip', 'link', 'set', name, 'down')
      guest('ip', 'link', 'set', name, 'name', "freshrole#{index}")
    end
  end

  it 'configures a first install through the original naming and role templates' do
    with_real_network do |directory, prior, current|
      remove_initial_role_configuration
      activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                             domain: 'home.goodkind.io')
      expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/20-enmgmt0.network')
      expect(role_state('enmwanbr0').fetch('NetworkFile')).to eq('/etc/systemd/network/20-enmwanbr0.network')
      expect(guest('systemctl', 'show', 'mwan-ifmgr@wan', '--property=ActiveState', '--value').strip).to eq('active')
    end
  end

  it 'restores absent configuration and stops the new writer after first-install staging fails' do
    with_real_network do |directory, prior, current|
      remove_initial_role_configuration
      expect do
        activation_play(directory, 'stage_failure', prior, current, prepared: true, corrupt: false,
                                                                    domain: 'home.goodkind.io')
      end.to raise_error(RuntimeError, /prior role inputs and applied state passed recovery verification/)
      expect(guest('find', '/etc/mwan', '-name', 'config.toml', '-o', '-name', 'network.json')).to eq('')
      expect(role_state('enmgmt0').fetch('NetworkFile')).to eq('/etc/systemd/network/10-mgmt.network')
      expect(role_state('enmwanbr0').fetch('NetworkFile')).to eq('/etc/systemd/network/40-mwanbr.network')
      expect(guest('systemctl', 'show', 'mwan-ifmgr@wan', '--property=ActiveState', '--value').strip).to eq('inactive')
      expect(guest('systemctl', 'show', 'mwan-ifmgr@wan', '--property=UnitFileState', '--value').strip).to eq('disabled')
    end
  end

  %w[partial malformed].each do |failure|
    it "rejects a #{failure} prior runtime pair before staging" do
      with_real_network do |directory, prior, current|
        before = %w[enmgmt0 enmwanbr0].map { |name| role_state(name).fetch('NetworkFile') }
        legacy = guest('sha256sum', '/etc/systemd/network/10-mgmt.network',
                       '/etc/systemd/network/40-mwanbr.network')
        if failure == 'partial'
          guest('rm', '/etc/mwan/config.toml')
          expected = /Both runtime configuration files must exist or both must be absent/
        else
          guest('truncate', '-s', '0', '/etc/mwan/network.json')
          expected = /Validate the prior network document before staging/
        end
        rejected_network = guest('cat', '/etc/mwan/network.json')
        expect do
          activation_play(directory, 'activate', prior, current, prepared: true, corrupt: false,
                                                                 domain: 'home.goodkind.io')
        end.to raise_error(RuntimeError, expected)
        expect(guest('cat', '/etc/mwan/network.json')).to eq(rejected_network)
        expect(guest('sha256sum', '/etc/systemd/network/10-mgmt.network',
                     '/etc/systemd/network/40-mwanbr.network')).to eq(legacy)
        expect(%w[enmgmt0 enmwanbr0].map { |name| role_state(name).fetch('NetworkFile') }).to eq(before)
      end
    end
  end
end
