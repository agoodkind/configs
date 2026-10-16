# frozen_string_literal: true

require 'tmpdir'
require_relative '../support/ansible_render'
require_relative '../support/command_runner'

# reboot-guest.yml imports tasks/reboot-and-require-new-boot.yml.
# The deploy-mwan playbook imports the same task file.
# reboot-guest.yml reboots exactly one QA Tack guest.
# These examples run the playbook in check mode against local inventories.
module RebootGuestPlaybook
  PLAYBOOKS = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks')
  PLAYBOOK_FILE = File.join(PLAYBOOKS, 'reboot-guest.yml')
  REQUEST_TASK = 'Refuse a guest reboot outside one QA Tack guest'
  CHECK_END_TASK = 'End a check run before the reboot'
  SCHEDULE_TASK = 'Schedule the reboot independently of the SSH connection'
  REFUSAL_MESSAGE = 'reboot-guest reboots exactly one QA Tack guest'
  QA_GUEST_INVENTORY = <<~INVENTORY
    [tack_all]
    reboot-test ansible_connection=local

    [tack_qa_all]
    reboot-test

    [tack_prod_all]
  INVENTORY
  REFUSED_INVENTORIES = {
    'two QA guests' => <<~INVENTORY,
      [tack_all]
      reboot-test-one ansible_connection=local
      reboot-test-two ansible_connection=local

      [tack_qa_all]
      reboot-test-one
      reboot-test-two

      [tack_prod_all]
    INVENTORY
    'a production guest' => <<~INVENTORY,
      [tack_all]
      reboot-test ansible_connection=local

      [tack_qa_all]

      [tack_prod_all]
      reboot-test
    INVENTORY
    'a host outside the QA and production groups' => <<~INVENTORY
      [tack_all]
      reboot-test ansible_connection=local

      [tack_qa_all]

      [tack_prod_all]
    INVENTORY
  }.freeze
  RUN_TIMEOUT_SECONDS = 120

  module_function

  def check_run(inventory_text)
    Dir.mktmpdir('reboot-guest') do |directory|
      inventory = File.join(directory, 'inventory.ini')
      File.write(inventory, inventory_text)
      password = File.join(directory, 'vault-password')
      File.write(password, AnsibleRender::VAULT_PASSWORD_PLACEHOLDER, perm: AnsibleRender::SECRET_FILE_MODE)
      argv = [AnsibleRender::PLAYBOOK_COMMAND, '--check', '--inventory', inventory, PLAYBOOK_FILE]
      CommandRunner.run({ AnsibleRender::VAULT_PASSWORD_ENV => password }, argv,
                        chdir: AnsibleRender::ANSIBLE_DIRECTORY, timeout_seconds: RUN_TIMEOUT_SECONDS)
    end
  end
end

RSpec.describe RebootGuestPlaybook do
  it 'refuses two QA guests, a production guest, and a host outside the QA and production groups',
     :aggregate_failures do
    RebootGuestPlaybook::REFUSED_INVENTORIES.each do |label, inventory_text|
      result = described_class.check_run(inventory_text)

      expect(result.exit_status.success?).to be(false), label
      expect(result.output).to include(RebootGuestPlaybook::REFUSAL_MESSAGE), label
    end
  end

  it 'passes the request check, ends the check run before the reboot, and schedules no reboot on one QA guest',
     :aggregate_failures do
    result = described_class.check_run(RebootGuestPlaybook::QA_GUEST_INVENTORY)

    expect(result.exit_status.success?).to be(true), result.output
    expect(result.output).to include("TASK [#{RebootGuestPlaybook::REQUEST_TASK}]")
    expect(result.output).to include("TASK [#{RebootGuestPlaybook::CHECK_END_TASK}]")
    expect(result.output).not_to include("TASK [#{RebootGuestPlaybook::SCHEDULE_TASK}]")
  end
end
