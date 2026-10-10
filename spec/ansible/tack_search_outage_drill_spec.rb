# frozen_string_literal: true

require 'json'
require 'tmpdir'
require_relative '../support/ansible_render'
require_relative '../support/command_runner'

# Each example runs the playbook in check mode against a temporary inventory with one local host.
module TackSearchOutageDrill
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tack-search-outage-drill.yml')
  REFUSAL_TASK = 'TASK [Refuse a search outage drill outside the QA search members or the outage range]'
  REFUSAL_MESSAGE = 'The playbook rejects an outage request'
  STOP_TASK = 'TASK [Stop the OpenSearch member]'
  SEARCH_GROUP = 'tack_search1_suburban_servers'
  QA_GROUP = 'tack_qa_all'
  PRODUCTION_GROUP = 'tack_prod_all'
  MEMBER = 'search-member ansible_connection=local ansible_become=false'
  RUN_TIMEOUT_SECONDS = 120

  module_function

  def inventory_text(member_groups)
    sections = [SEARCH_GROUP, QA_GROUP, PRODUCTION_GROUP].map do |group|
      next "[#{group}]\n#{MEMBER}\n" if group == SEARCH_GROUP || member_groups.include?(group)

      "[#{group}]\n"
    end
    sections.join("\n")
  end

  def variables(directory, seconds)
    { 'tack_search_outage_seconds' => seconds, 'tack_install_dir' => directory,
      'tack_search_member_url' => 'https://localhost:9200', 'tack_search_username' => 'drill',
      'tack_search_password' => 'unused', 'tack_search_ca_path' => File.join(directory, 'ca.pem') }
  end

  def run_drill(seconds:, member_groups:)
    Dir.mktmpdir('search-outage-drill') do |directory|
      inventory = File.join(directory, 'inventory.ini')
      File.write(inventory, inventory_text(member_groups))
      password = File.join(directory, 'vault-password')
      File.write(password, AnsibleRender::VAULT_PASSWORD_PLACEHOLDER, perm: AnsibleRender::SECRET_FILE_MODE)
      argv = [AnsibleRender::PLAYBOOK_COMMAND, '--check', '--inventory', inventory, PLAYBOOK_FILE,
              '--extra-vars', JSON.generate(variables(directory, seconds))]
      CommandRunner.run({ AnsibleRender::VAULT_PASSWORD_ENV => password }, argv,
                        chdir: AnsibleRender::ANSIBLE_DIRECTORY, timeout_seconds: RUN_TIMEOUT_SECONDS)
    end
  end
end

RSpec.describe TackSearchOutageDrill do
  def expect_refusal(result)
    expect(result.exit_status.success?).to be(false)
    expect(result.output).to include(TackSearchOutageDrill::REFUSAL_TASK, TackSearchOutageDrill::REFUSAL_MESSAGE)
    expect(result.output).not_to include(TackSearchOutageDrill::STOP_TASK)
  end

  it 'accepts a QA search member with an outage of 30 seconds', :aggregate_failures do
    result = described_class.run_drill(seconds: 30, member_groups: [TackSearchOutageDrill::QA_GROUP])

    expect(result.exit_status.success?).to be(true), result.output
    expect(result.output).to include(TackSearchOutageDrill::STOP_TASK)
    expect(result.output).not_to include(TackSearchOutageDrill::REFUSAL_MESSAGE)
  end

  it 'refuses a search member in tack_prod_all', :aggregate_failures do
    groups = [TackSearchOutageDrill::QA_GROUP, TackSearchOutageDrill::PRODUCTION_GROUP]

    expect_refusal(described_class.run_drill(seconds: 30, member_groups: groups))
  end

  it 'refuses a search member in neither tack_qa_all nor tack_prod_all', :aggregate_failures do
    expect_refusal(described_class.run_drill(seconds: 30, member_groups: []))
  end

  it 'refuses 29 seconds', :aggregate_failures do
    expect_refusal(described_class.run_drill(seconds: 29, member_groups: [TackSearchOutageDrill::QA_GROUP]))
  end

  it 'refuses 601 seconds', :aggregate_failures do
    expect_refusal(described_class.run_drill(seconds: 601, member_groups: [TackSearchOutageDrill::QA_GROUP]))
  end

  it 'refuses a duration of abc', :aggregate_failures do
    expect_refusal(described_class.run_drill(seconds: 'abc', member_groups: [TackSearchOutageDrill::QA_GROUP]))
  end
end
