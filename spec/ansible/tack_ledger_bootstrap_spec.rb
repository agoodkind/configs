# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# A from-empty ledger has no audit outbox until provision runs, and the two
# audited ledger commands record to it before they run. These checks prove the
# bootstrap run only starts the node and every other run keeps both commands.
module TackLedgerBootstrap
  PLAYBOOK_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'deploy-tack.yml')
  NODE_TASK_NAME = 'Prepare, start, and wait on the ledger node (one guest at a time; first start joins)'
  ROTATION_WAIT_TASK_NAME = 'Wait until every ledger node is healthy (certificate rotation)'
  GUARD_TASK_NAME = 'Refuse the ledger bootstrap on a run with a production guest'
  PRODUCTION_HOSTS = %w[tack tack-data1 tack-data2 tack-data3].freeze
  QA_HOSTS = %w[tack-qa tack-data1-suburban tack-data2-suburban tack-data3-suburban].freeze
  GROUPS = { 'tack_prod_all' => PRODUCTION_HOSTS, 'tack_qa_all' => QA_HOSTS }.freeze
  IDENTITY_FLAGS = '--operator-id render-only-operator'
  START_ONLY = %w[docker compose up -d yugabyte].freeze
  PREPARE = "docker compose run --rm tack-ops ops ledger node-prepare --execute #{IDENTITY_FLAGS};".split.freeze
  WAIT = "docker compose run --rm tack-ops ops ledger node-wait --execute #{IDENTITY_FLAGS}".split.freeze
  START_AFTER_PREPARE = ['prepare_status=$?;', *START_ONLY, '&&', '(exit', '"$prepare_status")'].freeze

  module_function

  # Every task of every play, including the tasks of a block.
  def tasks_in(tasks)
    tasks.flat_map { |task| [task] + tasks_in(task['block'] || []) + tasks_in(task['always'] || []) }
  end

  def task_named(name)
    plays = YAML.safe_load_file(PLAYBOOK_FILE)
    task = plays.flat_map { |play| tasks_in(play['tasks'] || []) }.find { |candidate| candidate['name'].to_s == name }
    raise "#{PLAYBOOK_FILE} has no task named #{name.inspect}" if task.nil?

    task
  end

  def variables(bootstrap:, rotated:)
    {
      'tack_ledger_bootstrap' => bootstrap,
      'tack_ledger_node_certificate_rotated' => rotated,
      'tack_ops_identity_flags' => IDENTITY_FLAGS
    }
  end

  # The shell command the node task runs, split on whitespace, and whether the
  # rotation wait runs, both from the real playbook text.
  def rendered(bootstrap:, rotated:)
    node_task = task_named(NODE_TASK_NAME)
    render = TaskExpressions.render_task(node_task, 'command' => node_task.dig('ansible.builtin.shell', 'cmd'))
    conditions = { 'rotation_wait' => TaskExpressions.condition_list(task_named(ROTATION_WAIT_TASK_NAME)['when']) }
    result = TaskExpressions.evaluate(
      variables: variables(bootstrap: bootstrap, rotated: rotated), facts: [], conditions: conditions, renders: [render]
    )
    { 'command' => result['renders'][0]['command'].split, 'rotation_wait' => result['conditions']['rotation_wait'] }
  end

  # The first play: the guard must run there, on the controller, before the
  # prepared-guests import and every play that changes a guest.
  def first_play
    YAML.safe_load_file(PLAYBOOK_FILE).first
  end

  # The guard's assertion evaluated for a run over play_hosts, from the real
  # playbook text. False means the assert fails the run.
  def guard_passes(bootstrap:, play_hosts:)
    guard = first_play.fetch('tasks').find { |task| task['name'].to_s == GUARD_TASK_NAME }
    raise "the first play of #{PLAYBOOK_FILE} has no task named #{GUARD_TASK_NAME.inspect}" if guard.nil?

    that = TaskExpressions.condition_list(guard.dig('ansible.builtin.assert', 'that'))
    result = TaskExpressions.evaluate(
      variables: variables(bootstrap: bootstrap, rotated: false).merge(
        'ansible_play_hosts_all' => play_hosts, 'groups' => GROUPS
      ),
      facts: [], conditions: { 'guard' => that }
    )
    result['conditions']['guard']
  end

  # Each guard case: the flag, the play hosts, and whether the assert passes.
  GUARD_CASES = [
    [true, PRODUCTION_HOSTS, false],
    [true, QA_HOSTS + [PRODUCTION_HOSTS.last], false],
    [true, QA_HOSTS, true],
    [false, PRODUCTION_HOSTS, true]
  ].freeze
  FULL_SEQUENCE = PREPARE + START_AFTER_PREPARE + ['&&'] + WAIT
end

RSpec.describe TackLedgerBootstrap do
  it 'runs the production guard once on the controller as the first task of the first play', :aggregate_failures do
    play = described_class.first_play
    first_task = play.fetch('tasks').first

    expect(play['hosts']).to eq('tack_all')
    expect(first_task['name']).to eq(TackLedgerBootstrap::GUARD_TASK_NAME)
    expect(first_task['run_once']).to be(true)
    expect(first_task['delegate_to']).to eq('localhost')
  end

  it 'refuses the flag on any run that includes a production guest', :aggregate_failures do
    TackLedgerBootstrap::GUARD_CASES.each do |bootstrap, play_hosts, passes|
      expect(described_class.guard_passes(bootstrap: bootstrap, play_hosts: play_hosts)).to be(passes),
                                                                                            "bootstrap=#{bootstrap} hosts=#{play_hosts}"
    end
  end

  it 'only starts the node and skips the rotation wait on a from-empty rebuild', :aggregate_failures do
    [false, true].each do |rotated|
      result = described_class.rendered(bootstrap: true, rotated: rotated)

      expect(result['command']).to eq(TackLedgerBootstrap::START_ONLY), "rotated=#{rotated}"
      expect(result['rotation_wait']).to be(false), "rotated=#{rotated}"
    end
  end

  it 'prepares, starts, and waits on the node in that order on every other run' do
    result = described_class.rendered(bootstrap: false, rotated: false)

    expect(result).to eq('command' => TackLedgerBootstrap::FULL_SEQUENCE, 'rotation_wait' => false)
  end

  it 'leaves the wait to the rotation task after a certificate rotation' do
    result = described_class.rendered(bootstrap: false, rotated: true)

    expect(result).to eq('command' => TackLedgerBootstrap::PREPARE + TackLedgerBootstrap::START_AFTER_PREPARE, 'rotation_wait' => true)
  end
end
