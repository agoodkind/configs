# frozen_string_literal: true

require 'tmpdir'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/command_runner'
require_relative '../support/task_expressions'

# reboot-guest reboots one QA Tack guest through the reboot tasks deploy-mwan
# imports. These examples evaluate the real request check, read the real
# second play, and run the real playbook in check mode on a local inventory.
module RebootGuestPlaybook
  PLAYBOOKS = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks')
  PLAYBOOK_FILE = File.join(PLAYBOOKS, 'reboot-guest.yml')
  REBOOT_TASK_FILE = 'tasks/reboot-and-require-new-boot.yml'
  REQUEST_TASK = 'Refuse a guest reboot outside one QA Tack guest'
  CHECK_END_TASK = 'End a check run before the reboot'
  SCHEDULE_TASK = 'Schedule the reboot independently of the SSH connection'
  IMPORT_KEY = 'ansible.builtin.import_tasks'
  META_KEY = 'ansible.builtin.meta'
  QA_GUEST = 'tack-app2.suburban.goodkind.io'
  QA_HOSTS = [QA_GUEST, 'tack-qa.suburban.goodkind.io'].freeze
  PRODUCTION_GUEST = 'tack-app2.home.goodkind.io'
  OTHER_HOST = 'proxy.home.goodkind.io'
  GROUPS = { 'tack_qa_all' => QA_HOSTS, 'tack_prod_all' => [PRODUCTION_GUEST] }.freeze
  REFUSED_REQUESTS = {
    'two QA guests' => QA_HOSTS,
    'a production guest' => [PRODUCTION_GUEST],
    'a host outside the Tack groups' => [OTHER_HOST]
  }.freeze
  CHECK_INVENTORY = <<~INVENTORY
    [tack_all]
    reboot-test ansible_connection=local

    [tack_qa_all]
    reboot-test

    [tack_prod_all]
  INVENTORY
  RUN_TIMEOUT_SECONDS = 120

  module_function

  def plays
    YAML.safe_load_file(PLAYBOOK_FILE)
  end

  # Whether the request check passes for the selected hosts, from the real assert.
  def request_passes(hosts)
    request = plays.first.fetch('tasks').find { |task| task['name'] == REQUEST_TASK }
    that = TaskExpressions.condition_list(request.dig('ansible.builtin.assert', 'that'))
    variables = { 'ansible_play_hosts_all' => hosts, 'groups' => GROUPS }
    TaskExpressions.evaluate(variables: variables, facts: [], conditions: { 'request' => that })
                   .dig('conditions', 'request')
  end

  def check_run
    Dir.mktmpdir('reboot-guest') do |directory|
      inventory = File.join(directory, 'inventory.ini')
      File.write(inventory, CHECK_INVENTORY)
      password = File.join(directory, 'vault-password')
      File.write(password, AnsibleRender::VAULT_PASSWORD_PLACEHOLDER, perm: AnsibleRender::SECRET_FILE_MODE)
      argv = [AnsibleRender::PLAYBOOK_COMMAND, '--check', '--inventory', inventory, PLAYBOOK_FILE]
      CommandRunner.run({ AnsibleRender::VAULT_PASSWORD_ENV => password }, argv,
                        chdir: AnsibleRender::ANSIBLE_DIRECTORY, timeout_seconds: RUN_TIMEOUT_SECONDS)
    end
  end
end

RSpec.describe RebootGuestPlaybook do
  it 'accepts one QA Tack guest' do
    expect(described_class.request_passes([RebootGuestPlaybook::QA_GUEST])).to be(true)
  end

  it 'refuses two guests, a production guest, and a host outside the Tack groups', :aggregate_failures do
    RebootGuestPlaybook::REFUSED_REQUESTS.each do |label, hosts|
      expect(described_class.request_passes(hosts)).to be(false), label
    end
  end

  it 'ends a check run and imports the shared reboot tasks as the only tasks of the second play', :aggregate_failures do
    check_end, reboot = described_class.plays[1].fetch('tasks')

    expect(described_class.plays[1].fetch('tasks').size).to eq(2)
    expect(check_end.except('name')).to eq(RebootGuestPlaybook::META_KEY => 'end_host', 'when' => 'ansible_check_mode')
    expect(reboot.except('name')).to eq(RebootGuestPlaybook::IMPORT_KEY => RebootGuestPlaybook::REBOOT_TASK_FILE)
  end

  it 'passes the request check and schedules no reboot in a check run on one QA guest', :aggregate_failures do
    result = described_class.check_run

    expect(result.exit_status.success?).to be(true), result.output
    expect(result.output).to include("TASK [#{RebootGuestPlaybook::REQUEST_TASK}]")
    expect(result.output).not_to include("TASK [#{RebootGuestPlaybook::SCHEDULE_TASK}]")
  end
end
