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

  # The guest part of a staged configuration check, rendered from the real
  # task with a staged file of size_bytes.
  def staged_check_guest_argv(name, file, size_bytes)
    command = task(name, 'deploy-routing-simulator-services.yml')
    staged = { 'results' => [{ 'item' => [file], 'stat' => { 'size' => size_bytes } }] }
    script = File.join(AnsibleRender::REPOSITORY_ROOT, 'testbed', 'routing-simulators', 'check-received-config.sh')
    argv = RoutingSimulatorConfig.fact({ 'argv' => command.fetch('ansible.builtin.command').fetch('argv') },
                                       { 'rsim_staged' => staged, 'rsim_received_config_script' => script })
    rendered = RoutingSimulatorConfig.evaluate_node(inventory, 'tunnel_vps_vps', facts: [argv]).fetch('facts').fetch('argv')
    rendered.drop(rendered.index('--') + 1).map(&:to_s)
  end

  def run_guest_check(argv, stdin_data)
    CommandRunner.capture(argv, stdin_data: stdin_data, chdir: AnsibleRender::REPOSITORY_ROOT,
                                timeout_seconds: AnsibleRender::PLAYBOOK_TIMEOUT_SECONDS)
  end

  def vmid(node)
    service = inventory.fetch('testbed_routing_nodes').fetch(node).fetch('service')
    inventory.fetch('service_mapping').fetch(service).fetch('vmid').to_s
  end

  def pushes?(staged_checksum, guest_output)
    collect = task('Collect the files with a changed checksum', 'deploy-routing-simulator-checksums.yml')
    push = task('Push each file with a changed checksum', 'deploy-routing-simulator-push.yml')
    file = ['nftables.conf', '/etc/nftables.conf.candidate', '0644', 'nftables']
    variables = { 'item' => file, 'rsim_files' => [file],
                  'rsim_staged' => { 'results' => [{ 'stat' => { 'checksum' => staged_checksum } }] },
                  'rsim_installed' => { 'results' => [{ 'stdout' => guest_output }] } }
    TaskExpressions.evaluate(
      variables: variables, facts: [TaskExpressions.fact_task(collect)],
      conditions: { 'push' => TaskExpressions.condition_list(push.fetch('when')) }
    ).fetch('conditions').fetch('push')
  end

  def converge(directory, run_name, before_apply_command, options = [])
    password_file = File.join(AnsibleRender::FIXTURE_DIRECTORY, 'unused-vault-password.txt')
    extra_vars = { 'work_directory' => directory, 'run_name' => run_name, 'before_apply_command' => before_apply_command }
    AnsibleRender.run_playbook(password_file, 'localhost,', 'routing_simulator_converge.yml', extra_vars,
                               AnsibleRender::PLAYBOOK_TIMEOUT_SECONDS, options)
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

  {
    'Check the staged simulator firewall' => ['nftables.conf', %w[nft -c -f]],
    'Check the staged FRR configuration' => ['frr.conf', %w[vtysh -C -f]]
  }.each do |name, (file, checker)|
    it "passes staged content through the guest wrapper with the staged size in #{name}", :aggregate_failures do
      content = "flush ruleset\n"
      guest = staged_check_guest_argv(name, file, content.bytesize)
      guard = guest.first(guest.length - checker.length)

      complete = run_guest_check(guard + ['cat'], content)
      expect(complete.exit_status).to be_success, complete.error_output
      expect(complete.output).to eq(content)
      [content.chop, ''].each do |received|
        short = run_guest_check(guard + ['cat'], received)
        expect(short.exit_status).not_to be_success, received.inspect
        expect(short.output).to eq(''), received.inspect
      end
    end
  end

  it 'pushes a file only when the guest checksum differs', :aggregate_failures do
    expect(pushes?('abc123', "abc123  /etc/nftables.conf.candidate\n")).to be(false)
    expect(pushes?('abc123', "def456  /etc/nftables.conf.candidate\n")).to be(true)
    expect(pushes?('abc123', '')).to be(true)
  end

  it 'reports only the changed file in check mode and changes no guest file or group', :aggregate_failures do
    Dir.mktmpdir('routing-converge') do |directory|
      %w[staged guest applied].each { |name| Dir.mkdir(File.join(directory, name)) }
      %w[first second].each { |name| File.write(File.join(directory, 'staged', "#{name}.conf"), "#{name}\n") }
      File.write(File.join(directory, 'guest', 'first.conf'), "first\n")
      File.write(File.join(directory, 'guest', 'second.conf'), "old\n")

      checked = converge(directory, 'checked', 'true', ['--check'])
      expect(checked.exit_status).to be_success, checked.output
      expect(checked.output).to include("Check mode skips the push of #{directory}/guest/second.conf")
      expect(checked.output).not_to include("Check mode skips the push of #{directory}/guest/first.conf")
      expect(File.read(File.join(directory, 'guest', 'second.conf'))).to eq("old\n")
      expect(applied(directory)).to eq([])
    end
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
