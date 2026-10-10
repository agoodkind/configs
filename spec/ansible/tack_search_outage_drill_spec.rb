# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# The outage drill must exclude production hosts and restart OpenSearch without recreating the container.
module TackSearchOutageDrill
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tack-search-outage-drill.yml')
  REQUEST_TASK = 'Refuse a search outage drill outside the QA search members or the outage range'
  QA_MEMBER = 'tack-search1.suburban.goodkind.io'
  PRODUCTION_MEMBER = 'tack-search1.home.goodkind.io'
  GROUPS = { 'tack_qa_all' => [QA_MEMBER, 'tack-qa.suburban.goodkind.io'],
             'tack_prod_all' => [PRODUCTION_MEMBER, 'tack.home.goodkind.io'] }.freeze

  module_function

  def plays
    YAML.safe_load_file(PLAYBOOK_FILE)
  end

  def task(tasks, name)
    found = tasks.find { |candidate| candidate['name'] == name }
    raise "#{PLAYBOOK_FILE} has no task named #{name.inspect}" if found.nil?

    found
  end

  def request_passes(**overrides)
    variables = { 'tack_search_outage_seconds' => 60, 'ansible_play_hosts_all' => [QA_MEMBER], 'groups' => GROUPS }
    that = TaskExpressions.condition_list(task(plays.first.fetch('tasks'), REQUEST_TASK).dig('ansible.builtin.assert', 'that'))
    TaskExpressions.evaluate(variables: variables.merge(overrides), facts: [], conditions: { 'request' => that })
                   .dig('conditions', 'request')
  end
end

RSpec.describe TackSearchOutageDrill do
  it 'accepts a QA search member with an outage inside the range' do
    expect(described_class.request_passes).to be(true)
  end

  it 'refuses a production host' do
    expect(described_class.request_passes('ansible_play_hosts_all' => [TackSearchOutageDrill::PRODUCTION_MEMBER])).to be(false)
  end
end
