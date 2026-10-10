# frozen_string_literal: true

require 'tmpdir'
require_relative '../support/routing_simulator_config'

RSpec.describe 'routing simulator file deploy' do
  let(:inventory) { RoutingSimulatorConfig.inventory }

  def task(name, file)
    RoutingSimulatorConfig.task(name, 'tasks', file)
  end

  def files(node)
    collect = TaskExpressions.fact_task(task('Collect the simulator files', 'routing-simulator-file-list.yml'))
    RoutingSimulatorConfig.evaluate_node(inventory, node, facts: [collect]).fetch('facts').fetch('rsim_files')
  end

  def service_argv(name, node)
    collect = TaskExpressions.fact_task(task('Collect the simulator files', 'routing-simulator-file-list.yml'))
    command = task(name, 'deploy-routing-simulator-services.yml')
    argv = RoutingSimulatorConfig.fact({ 'argv' => command.fetch('ansible.builtin.command').fetch('argv') })
    RoutingSimulatorConfig.evaluate_node(inventory, node, facts: [collect, argv]).fetch('facts').fetch('argv')
  end

  def vmid(node)
    service = inventory.fetch('testbed_routing_nodes').fetch(node).fetch('service')
    inventory.fetch('service_mapping').fetch(service).fetch('vmid').to_s
  end

  def pushes?(staged_checksum, guest_output)
    push = task('Push each file with a changed checksum', 'deploy-routing-simulator-push.yml')
    item = [[], { 'stat' => { 'checksum' => staged_checksum } }, { 'stdout' => guest_output }]
    TaskExpressions.evaluate(
      variables: { 'item' => item }, facts: [],
      conditions: { 'push' => TaskExpressions.condition_list(push.fetch('when')) }
    ).fetch('conditions').fetch('push')
  end

  def converge(directory, run_name, before_apply_command)
    password_file = File.join(AnsibleRender::FIXTURE_DIRECTORY, 'unused-vault-password.txt')
    extra_vars = { 'work_directory' => directory, 'run_name' => run_name, 'before_apply_command' => before_apply_command }
    AnsibleRender.run_playbook(password_file, 'localhost,', 'routing_simulator_converge.yml', extra_vars,
                               AnsibleRender::PLAYBOOK_TIMEOUT_SECONDS)
  end

  def applied(directory)
    Dir.children(File.join(directory, 'applied')).sort
  end

  it 'lists the FRR and tracker files only for a node that needs them', :aggregate_failures do
    vps_files = files('tunnel_static_vps')

    expect(vps_files.map(&:last).uniq).to contain_exactly('nftables', 'sysctl', 'network', 'frr', 'tracker')
    expect(vps_files.map { |file| file[1] }).to include(
      '/etc/systemd/network/40-rsim-sit-sonic1.netdev', '/etc/systemd/system/routing-home-route-0.service'
    )
    expect(files('tunnel_static_client').map(&:last).uniq).to contain_exactly('nftables', 'sysctl', 'network')
  end

  it 'loads only the pushed simulator sysctl file in the guest' do
    node = 'tunnel_static_vps'
    sysctl_path = files(node).find { |file| file.last == 'sysctl' }[1]

    expect(service_argv('Apply the simulator sysctl values', node))
      .to eq(['pct', 'exec', vmid(node), '--', 'sysctl', '-p', sysctl_path])
  end

  it 'reads the FRR version from the package database in the guest' do
    node = 'tunnel_static_vps'

    expect(service_argv('Read the FRR version', node).map(&:to_s))
      .to eq(['pct', 'exec', vmid(node), '--', 'dpkg-query', '--show', '--showformat', 'FRR ${Version}', 'frr'])
  end

  it 'pushes a file only when the guest checksum differs', :aggregate_failures do
    expect(pushes?('abc123', "abc123  /etc/nftables.conf.candidate\n")).to be(false)
    expect(pushes?('abc123', "def456  /etc/nftables.conf.candidate\n")).to be(true)
    expect(pushes?('abc123', '')).to be(true)
  end

  it 'applies a pushed file on the run after a failed run and only a changed group after that', :aggregate_failures do
    Dir.mktmpdir('routing-converge') do |directory|
      %w[staged guest applied].each { |name| Dir.mkdir(File.join(directory, name)) }
      %w[first second].each { |name| File.write(File.join(directory, 'staged', "#{name}.conf"), "#{name}\n") }

      failed = converge(directory, 'failed', 'false')
      expect(failed.exit_status).not_to be_success, failed.output
      expect(File.read(File.join(directory, 'guest', 'first.conf'))).to eq("first\n")
      expect(applied(directory)).to eq([])

      repeated = converge(directory, 'repeated', 'true')
      expect(repeated.exit_status).to be_success, repeated.output
      expect(applied(directory)).to eq(%w[repeated-first repeated-second])

      unchanged = converge(directory, 'unchanged', 'true')
      expect(unchanged.exit_status).to be_success, unchanged.output
      expect(applied(directory)).to eq(%w[repeated-first repeated-second])

      File.write(File.join(directory, 'staged', 'second.conf'), "changed\n")
      changed = converge(directory, 'changed', 'true')
      expect(changed.exit_status).to be_success, changed.output
      expect(applied(directory)).to eq(%w[changed-second repeated-first repeated-second])
    end
  end
end
