# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# The released mwan binary writes and enables the units, drop-ins, schema, and
# access policy it embeds through `mwan install --role <role> --apply`
# (MWAN-391). These checks read the real playbooks: each host role runs the
# verb once, after the binary lands, and no task copies, enables, or installs
# into sysrepo anything the verb owns, so a second writer cannot come back
# unnoticed. The verb's change report is evaluated with ansible-core's own
# templar against lines the release binary prints.
module MwanInstall
  PLAYBOOK_DIRECTORY = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks')
  GROUP_VARS_DIRECTORY = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'inventory', 'group_vars')
  COMMAND_KEY = 'ansible.builtin.command'
  COPY_KEY = 'ansible.builtin.copy'
  FILE_WRITE_KEYS = [COPY_KEY, 'ansible.builtin.template'].freeze
  SYSTEMD_KEY = 'ansible.builtin.systemd'
  IMPORT_KEYS = ['ansible.builtin.import_tasks', 'ansible.builtin.include_tasks'].freeze
  NESTED_KEYS = %w[block rescue always].freeze
  BINARY = '/usr/local/bin/mwan'
  UNIT_DIRECTORY = '/etc/systemd/system'
  SCHEMA_DIRECTORY = '/usr/local/share/wanconfig/yang'
  UNIT_SUFFIX = '.service'
  TEMPLATE_MARKER = '{{'

  # Text that only a copy of a file the verb owns, or a schema read from this
  # repository instead of the release, would carry.
  FORBIDDEN_TEXT = [
    'third_party/yang',
    'mwan/yang/',
    'mwan-failover/mwan-ifmgr.service',
    'mwan/services/mwan-trace-boot.service',
    'mwan/wanconfig/rousette.service',
    'mwan/wanconfig/nghttpx-wanconfig.service',
    'mwan/wanconfig/nacm-anonymous.xml',
    'mwan/config/99-quiet-console.conf',
    'mwan/overrides/nftables.service.d-override.conf',
    'mwan/overrides/systemd-networkd.service.d-override.conf',
    'mwan/go/cmd/mwan/',
    'sysrepocfg',
    '--update',
    'wanconfig_yang'
  ].freeze

  # Each role, the task list that installs it, the host paths the verb writes
  # for it, and the units it enables, as `mwan install` with no flags prints
  # them for release 202609191802-8-085fc2b.
  ROLES = [
    {
      role: 'wan', file: 'deploy-mwan.yml', play: 'Configure MWAN VM',
      owned: %W[
        #{UNIT_DIRECTORY}/mwan-agent.service
        #{UNIT_DIRECTORY}/mwan-ifmgr@.service
        #{UNIT_DIRECTORY}/mwan-trace-boot.service
        #{UNIT_DIRECTORY}/rousette.service
        #{UNIT_DIRECTORY}/nghttpx-wanconfig.service
        #{UNIT_DIRECTORY}/nftables.service.d/override.conf
        #{UNIT_DIRECTORY}/systemd-networkd.service.d/override.conf
        /etc/sysctl.d/99-quiet-console.conf
        /etc/sysrepo-nacm-anonymous.xml
      ],
      enabled: %w[mwan-agent mwan-ifmgr@wan mwan-trace-boot rousette nghttpx-wanconfig]
    },
    {
      role: 'failover', file: 'deploy-mwan-failover.yml', play: 'Configure MWAN failover LXC',
      owned: %W[
        #{UNIT_DIRECTORY}/mwan-agent.service
        #{UNIT_DIRECTORY}/mwan-ifmgr.service
        #{UNIT_DIRECTORY}/mwan-ifmgr.service.d/lxc-failover.conf
      ],
      enabled: %w[mwan-agent mwan-ifmgr]
    },
    {
      role: 'host', file: 'tasks/proxmox-host.yml', play: nil,
      owned: ["#{UNIT_DIRECTORY}/mwan-ifmgr.service"],
      enabled: %w[mwan-ifmgr]
    }
  ].freeze

  # The gateway groups whose end-of-deploy enable list must leave the verb's
  # units to the verb.
  GATEWAY_GROUP_FILES = %w[mwan_servers.yml mwan_suburban_servers.yml].freeze

  # Lines `mwan install --apply` prints: "no change" when it wrote no file, one
  # line per written file, per module installed or updated, and per policy
  # import, then the units it enabled.
  REPORT_CASES = [
    { name: 'a run that changed nothing', roles: %w[wan failover host],
      lines: ['no change', 'enabled mwan-ifmgr.service'], want: false },
    { name: 'a run that wrote a unit', roles: %w[wan failover host],
      lines: ["wrote #{UNIT_DIRECTORY}/mwan-ifmgr.service", 'enabled mwan-ifmgr.service'], want: true },
    { name: 'a run that installed a schema module into an emptied repository', roles: %w[wan],
      lines: ['no change', 'installed module goodkind-mwan-steering@2026-09-19', 'enabled mwan-agent.service'],
      want: true },
    { name: 'a run that updated a schema module', roles: %w[wan],
      lines: ['no change', 'updated module goodkind-mwan-steering from 2026-09-14 to 2026-09-19',
              'enabled mwan-agent.service'],
      want: true },
    { name: 'a run that imported the access policy into a reset datastore', roles: %w[wan],
      lines: ['no change', 'imported the ietf-netconf-acm policy into startup and running',
              'enabled mwan-agent.service'],
      want: true }
  ].freeze

  module_function

  def file_path(name)
    File.join(PLAYBOOK_DIRECTORY, name)
  end

  def role_tasks(spec)
    path = file_path(spec[:file])
    loaded = YAML.safe_load_file(path)
    return flatten(loaded, File.dirname(path)) if spec[:play].nil?

    play = loaded.find { |candidate| candidate['name'] == spec[:play] }
    raise "#{path} has no play #{spec[:play].inspect}" if play.nil?

    flatten(play['tasks'], File.dirname(path))
  end

  # Every task in file order, with blocks opened and statically named task
  # files read in place, so a copy moved into an imported file is still seen.
  def flatten(tasks, directory)
    tasks.flat_map do |task|
      nested = NESTED_KEYS.flat_map { |key| flatten(task[key] || [], directory) }
      [task] + nested + imported(task, directory)
    end
  end

  def imported(task, directory)
    name = IMPORT_KEYS.map { |key| task[key] }.compact.first
    return [] if name.nil? || name.include?(TEMPLATE_MARKER)

    path = [File.join(directory, name), File.join(PLAYBOOK_DIRECTORY, name)].find { |candidate| File.file?(candidate) }
    raise "cannot resolve imported task file #{name.inspect} from #{directory}" if path.nil?

    flatten(YAML.safe_load_file(path), File.dirname(path))
  end

  def install_argv(role)
    [BINARY, 'install', '--role', role, '--apply']
  end

  # One field of a task's module arguments, or nil when the task does not use
  # that module or passes it the free-form string shape.
  def module_field(task, key, field)
    arguments = task[key]
    return nil unless arguments.is_a?(Hash)

    arguments[field]
  end

  def command_argv(task)
    module_field(task, COMMAND_KEY, 'argv')
  end

  def install_index(tasks, role)
    indexes = tasks.each_index.select { |index| command_argv(tasks[index]) == install_argv(role) }
    raise "want one #{install_argv(role).join(' ')} task, found #{indexes.size}" unless indexes.size == 1

    indexes.first
  end

  def binary_copy_index(tasks)
    tasks.index { |task| module_field(task, COPY_KEY, 'dest') == BINARY }
  end

  def written_destinations(tasks)
    FILE_WRITE_KEYS.flat_map { |key| tasks.map { |task| module_field(task, key, 'dest') } }.compact.map(&:to_s)
  end

  def unit_name(name)
    name.to_s.delete_suffix(UNIT_SUFFIX)
  end

  # Every unit a systemd task enables, reading its loop when the name is the
  # loop item.
  def enabled_units(tasks)
    tasks.flat_map do |task|
      next [] if module_field(task, SYSTEMD_KEY, 'enabled') != true

      names = task['loop'].is_a?(Array) ? task['loop'] : [module_field(task, SYSTEMD_KEY, 'name')]
      names.map { |name| unit_name(name) }
    end
  end

  def change_verdict(task, register, lines)
    result = TaskExpressions.command_result(0, lines.join("\n"), '').merge('stdout_lines' => lines)
    verdicts = TaskExpressions.evaluate(
      variables: { register => result }, facts: [],
      conditions: { 'changed' => TaskExpressions.condition_list(task['changed_when']) }
    )['conditions'] || {}
    raise "evaluator returned no verdict: #{verdicts.inspect}" unless verdicts.key?('changed')

    verdicts['changed']
  end
end

RSpec.describe MwanInstall do
  MwanInstall::ROLES.each do |spec|
    describe "the #{spec[:role]} role in #{spec[:file]}" do
      let(:tasks) { described_class.role_tasks(spec) }

      it 'runs mwan install once, after the binary lands' do
        install = described_class.install_index(tasks, spec[:role])
        binary = described_class.binary_copy_index(tasks)

        expect(binary).not_to be_nil, "#{spec[:file]} never copies #{MwanInstall::BINARY}"
        expect(install).to be > binary,
                           "the install is task #{install} and the binary copy is task #{binary}; " \
                           'the verb would run the previous release'
      end

      it 'writes no file the verb owns' do
        written = described_class.written_destinations(tasks)
        owned = written.select do |dest|
          spec[:owned].include?(dest) || dest.start_with?(MwanInstall::SCHEMA_DIRECTORY)
        end

        expect(owned).to be_empty, "#{spec[:file]} writes #{owned.join(', ')}, which mwan install owns"
      end

      it 'enables no unit the verb enables' do
        doubled = described_class.enabled_units(tasks) & spec[:enabled]

        expect(doubled).to be_empty, "#{spec[:file]} enables #{doubled.join(', ')}, which mwan install enables"
      end

      it 'reads no schema or unit from this repository and edits no sysrepo module or policy' do
        offending = tasks.filter_map do |task|
          text = YAML.dump(task.except(*MwanInstall::NESTED_KEYS))
          found = MwanInstall::FORBIDDEN_TEXT.select { |forbidden| text.include?(forbidden) }
          "#{task['name']}: #{found.join(', ')}" unless found.empty?
        end

        expect(offending).to be_empty, offending.join("\n")
      end

      MwanInstall::REPORT_CASES.select { |test_case| test_case[:roles].include?(spec[:role]) }.each do |test_case|
        it "reports a change for #{test_case[:name]} only when the verb changed something" do
          task = tasks[described_class.install_index(tasks, spec[:role])]
          verdict = described_class.change_verdict(task, task.fetch('register'), test_case[:lines])

          expect(verdict).to be(test_case[:want])
        end
      end
    end
  end

  it 'validates the gateway render before installing the MWAN management stack' do
    tasks = described_class.role_tasks(MwanInstall::ROLES.first)
    deploy_gate_copy = tasks.index { |task| task['name'] == 'Push the deploy-gate binary to the Proxmox delegate' }
    printed = tasks.index { |task| Array(described_class.command_argv(task)).include?('--print-schema') }
    render_copy = tasks.index { |task| task['name'] == 'Copy the rendered network configuration to the Proxmox delegate' }
    loader_check = tasks.index { |task| Array(described_class.command_argv(task)).include?('check-network') }
    cleanup = tasks.index { |task| task['name'] == 'Remove the schema directory on the Proxmox delegate' }
    management_stack_install = tasks.index { |task| task['ansible.builtin.import_tasks'] == 'tasks/mwan-vm/wanconfig-stack.yml' }

    expected_tasks = [deploy_gate_copy, printed, render_copy, loader_check, cleanup, management_stack_install]
    expect(expected_tasks).to all(be_a(Integer))
    expect(tasks[deploy_gate_copy]['check_mode']).to be(false)
    expect(printed).to be < render_copy
    expect(render_copy).to be < loader_check
    expect(described_class.module_field(tasks[render_copy], MwanInstall::COPY_KEY, 'src')).to eq(
      '{{ mwan_network_json_local }}'
    )
    expect(described_class.module_field(tasks[render_copy], MwanInstall::COPY_KEY, 'dest')).to eq(
      '{{ mwan_schema_remote.path }}/network.json'
    )
    expect(tasks[loader_check]['check_mode']).to be(false)
    expect(loader_check).to be < cleanup
    expect(cleanup).to be < management_stack_install
    expect(described_class.command_argv(tasks[loader_check])).to eq(
      [
        '/usr/local/sbin/mwan-deploy-gate',
        'deploy-gate',
        'check-network',
        '{{ mwan_schema_remote.path }}/network.json',
        '{{ mwan_schema_remote.path }}'
      ]
    )
  end

  MwanInstall::GATEWAY_GROUP_FILES.each do |group_file|
    it "leaves the verb's units out of the enable list in #{group_file}" do
      services = YAML.safe_load_file(File.join(MwanInstall::GROUP_VARS_DIRECTORY, group_file)).fetch('mwan_enabled_services')
      doubled = services.map { |name| described_class.unit_name(name) } & MwanInstall::ROLES.first[:enabled]

      expect(doubled).to be_empty
    end
  end
end
