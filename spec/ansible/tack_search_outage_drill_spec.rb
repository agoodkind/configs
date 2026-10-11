# frozen_string_literal: true

require 'json'
require 'tmpdir'
require_relative '../support/ansible_render'
require_relative '../support/command_runner'
require_relative '../support/tack_search_outage_compose'

module TackSearchOutageDrill
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tack-search-outage-drill.yml')
  REFUSAL_TASK = 'TASK [Refuse a search outage drill outside the QA search members or the outage range]'
  REFUSAL_MESSAGE = 'The playbook rejects an outage request'
  STOP_TASK = 'TASK [Stop the OpenSearch member]'
  EXIT_WAIT_TASK = 'TASK [Wait for the OpenSearch container to exit]'
  START_TASK = 'TASK [Start the OpenSearch member]'
  HEALTH_TASK = 'TASK [Wait until the OpenSearch cluster health is green or yellow]'
  KILL_FAILURE_MESSAGE = 'Docker sent signal 9'
  TASK_RESULT = /^(?<status>ok|changed|skipping|fatal): \[search-member\]/
  # One health attempt limits the duration of the failed wait because the Compose project serves no OpenSearch endpoint; the play continues to the failure tasks after the wait.
  SINGLE_HEALTH_ATTEMPT = { 'tack_search_outage_health_retries' => 1, 'tack_search_outage_health_delay_seconds' => 1,
                            'tack_search_outage_health_request_timeout_seconds' => 1 }.freeze
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
      run_playbook(directory, member_groups, ['--check'], variables(directory, seconds))
    end
  end

  def run_container_drill(directory, container)
    overrides = SINGLE_HEALTH_ATTEMPT.merge('tack_search_outage_container' => container)
    run_playbook(directory, [QA_GROUP], [], variables(directory, 30).merge(overrides))
  end

  def run_playbook(directory, member_groups, options, extra_variables)
    inventory = File.join(directory, 'inventory.ini')
    File.write(inventory, inventory_text(member_groups))
    password = File.join(directory, 'vault-password')
    File.write(password, AnsibleRender::VAULT_PASSWORD_PLACEHOLDER, perm: AnsibleRender::SECRET_FILE_MODE)
    argv = [AnsibleRender::PLAYBOOK_COMMAND, *options, '--inventory', inventory, PLAYBOOK_FILE,
            '--extra-vars', JSON.generate(extra_variables)]
    CommandRunner.run({ AnsibleRender::VAULT_PASSWORD_ENV => password }, argv,
                      chdir: AnsibleRender::ANSIBLE_DIRECTORY, timeout_seconds: RUN_TIMEOUT_SECONDS)
  end

  def task_status(output, task)
    section = output.split(task, 2).fetch(1)
    section.match(TASK_RESULT)[:status]
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

  it 'waits for a container that ignores the stop signal to exit before starting the container', :aggregate_failures do
    TackSearchOutageCompose.with_project do |directory, container|
      output = described_class.run_container_drill(directory, container).output

      expect(described_class.task_status(output, TackSearchOutageDrill::STOP_TASK)).to eq('changed'), output
      expect(described_class.task_status(output, TackSearchOutageDrill::EXIT_WAIT_TASK)).to eq('ok'), output
      expect(described_class.task_status(output, TackSearchOutageDrill::START_TASK)).to eq('changed'), output
      expect(output.index(TackSearchOutageDrill::EXIT_WAIT_TASK)).to be < output.index(TackSearchOutageDrill::START_TASK)
      expect(output).to include(TackSearchOutageDrill::HEALTH_TASK)
      expect(TackSearchOutageCompose.running?(directory, container)).to be(true)
    end
  end

  it 'fails after the start when Docker ends the OpenSearch member with signal 9', :aggregate_failures do
    TackSearchOutageCompose.with_project do |directory, container|
      result = described_class.run_container_drill(directory, container)
      output = result.output

      expect(result.exit_status.success?).to be(false), output
      expect(described_class.task_status(output, TackSearchOutageDrill::START_TASK)).to eq('changed'), output
      expect(output).to include(TackSearchOutageDrill::KILL_FAILURE_MESSAGE), output
      expect(TackSearchOutageCompose.running?(directory, container)).to be(true)
    end
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
