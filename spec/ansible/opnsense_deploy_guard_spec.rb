# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'

# The OPNsense deploy reads the router daemon through the hypervisor's
# opnsensectl, so a hypervisor without that binary fails every read the play
# makes. The install path writes no pending-verify marker and keeps no previous
# slot, so nothing reverts a guest the play has already changed. These checks
# pin that the hypervisor guard runs before the play touches the guest.
module OpnsenseDeployGuard
  TASK_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'tasks', 'mwan-opnsense-deploy.yml')
  IMPORT_KEY = 'ansible.builtin.import_tasks'
  VERIFY_TASK_FILE = 'verify-opnsensectl-release.yml'
  DELEGATE = '{{ mwan_proxmox_delegate }}'
  HYPERVISOR_BINARY = '/usr/local/bin/opnsensectl'
  # Every module below writes to the host it runs on, so the first task using
  # one is the first change to the guest.
  MUTATING_MODULES = [
    'ansible.builtin.copy',
    'ansible.builtin.file',
    'ansible.builtin.lineinfile',
    'ansible.builtin.command',
    'ansible.builtin.script'
  ].freeze

  module_function

  def tasks
    YAML.safe_load_file(TASK_FILE)
  end

  # The index of the guard: the delegated import of the hypervisor verify tasks.
  def guard_index(tasks)
    tasks.index do |task|
      task[IMPORT_KEY] == VERIFY_TASK_FILE && task['delegate_to'] == DELEGATE
    end
  end

  # The index of the first task that changes the guest: a mutating module with
  # no delegate_to, so it runs on the OPNsense host itself.
  def first_guest_change_index(tasks)
    tasks.index do |task|
      task['delegate_to'].nil? && MUTATING_MODULES.any? { |mod| task.key?(mod) }
    end
  end

  def guard_task(tasks)
    index = guard_index(tasks)
    index.nil? ? nil : tasks[index]
  end
end

RSpec.describe OpnsenseDeployGuard do
  let(:tasks) { described_class.tasks }

  it 'guards on the hypervisor before it changes the guest' do
    guard = described_class.guard_index(tasks)
    first_change = described_class.first_guest_change_index(tasks)

    expect(guard).not_to be_nil,
                         "#{OpnsenseDeployGuard::TASK_FILE} imports #{OpnsenseDeployGuard::VERIFY_TASK_FILE} " \
                         "delegated to #{OpnsenseDeployGuard::DELEGATE} nowhere"
    expect(first_change).not_to be_nil, "#{OpnsenseDeployGuard::TASK_FILE} changes the guest nowhere"
    expect(guard).to be < first_change,
                     "the hypervisor guard is task #{guard} and the first guest change is task #{first_change}; " \
                     'a failed guard would leave the guest already changed'
  end

  it 'checks the hypervisor binary the later reads use' do
    guard = described_class.guard_task(tasks)

    expect(guard).not_to be_nil
    expect(guard['vars']).to include('opnsensectl_installed_binary' => OpnsenseDeployGuard::HYPERVISOR_BINARY)
  end
end
