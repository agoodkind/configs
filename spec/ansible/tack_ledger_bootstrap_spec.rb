# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# A from-empty ledger has no audit outbox until the first node creates it, and
# the audited ledger commands record to it before they run. These checks prove
# the bootstrap run sequences the nodes and every other run keeps both commands.
module TackLedgerBootstrap
  PLAYBOOK_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'deploy-tack.yml')
  NODE_TASK_NAME = 'Prepare, start, and wait on the ledger node (one guest at a time; first start joins)'
  ROTATION_WAIT_TASK_NAME = 'Wait until every ledger node is healthy (certificate rotation)'
  GUARD_TASK_NAME = 'Refuse the ledger bootstrap on a run with a production guest'
  AUDIT_GUARD_TASK_NAME = 'Refuse the ledger audit bootstrap outside a from-empty rebuild'
  PRODUCTION_HOSTS = %w[tack tack-data1 tack-data2 tack-data3].freeze
  QA_HOSTS = %w[tack-qa tack-data1-suburban tack-data2-suburban tack-data3-suburban].freeze
  GROUPS = { 'tack_prod_all' => PRODUCTION_HOSTS, 'tack_qa_all' => QA_HOSTS }.freeze
  NODE_ADDRESSES = { 'yb1' => '3d06:bad:b01:210::220', 'yb2' => '3d06:bad:b01:210::221',
                     'yb3' => '3d06:bad:b01:210::222' }.freeze
  IDENTITY_FLAGS = '--operator-id render-only-operator'
  START_ONLY = %w[docker compose up -d yugabyte].freeze
  START_WAIT = %w[docker compose up -d --wait --wait-timeout 420 yugabyte].freeze
  TACK_OPS = %w[&& docker compose run --rm tack-ops ops ledger].freeze
  AUDIT_BOOTSTRAP = [*TACK_OPS, 'audit-bootstrap', '--execute', *IDENTITY_FLAGS.split].freeze
  PREPARE = "docker compose run --rm tack-ops ops ledger node-prepare --execute #{IDENTITY_FLAGS};".split.freeze
  WAIT = "docker compose run --rm tack-ops ops ledger node-wait --execute #{IDENTITY_FLAGS}".split.freeze
  START_AFTER_PREPARE = ['prepare_status=$?;', *START_ONLY, '&&', '(exit', '"$prepare_status")'].freeze
  FULL_SEQUENCE = PREPARE + START_AFTER_PREPARE + ['&&'] + WAIT

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

  def variables(bootstrap:, rotated: false, audit: false, node: 'yb1')
    {
      'tack_ledger_bootstrap' => bootstrap,
      'tack_ledger_audit_bootstrap' => audit,
      'tack_ledger_node_certificate_rotated' => rotated,
      'tack_ledger_node_name' => node,
      'tack_ledger_node_addresses' => NODE_ADDRESSES,
      'tack_ops_identity_flags' => IDENTITY_FLAGS
    }
  end

  # The bootstrap-wait command a later node runs, split on whitespace.
  def bootstrap_wait(count, replicas: false)
    [*TACK_OPS, 'bootstrap-wait', '--execute', '--masters', count.to_s, '--tablet-servers', count.to_s,
     *(replicas ? ['--replicas', count.to_s] : []), *IDENTITY_FLAGS.split]
  end

  # The shell command the node task runs, split on whitespace, and whether the
  # rotation wait runs, both from the real playbook text.
  def rendered(**)
    node_task = task_named(NODE_TASK_NAME)
    render = TaskExpressions.render_task(node_task, 'command' => node_task.dig('ansible.builtin.shell', 'cmd'))
    conditions = { 'rotation_wait' => TaskExpressions.condition_list(task_named(ROTATION_WAIT_TASK_NAME)['when']) }
    result = TaskExpressions.evaluate(variables: variables(**), facts: [], conditions: conditions, renders: [render])
    { 'command' => result['renders'][0]['command'].split, 'rotation_wait' => result['conditions']['rotation_wait'] }
  end

  # The first play: the guards must run there, on the controller, before the
  # prepared-guests import and every play that changes a guest.
  def first_play
    YAML.safe_load_file(PLAYBOOK_FILE).first
  end

  # A first-play guard's assertion evaluated for a run over play_hosts, from
  # the real playbook text. False means the assert fails the run.
  def guard_passes(name, play_hosts:, **)
    guard = first_play.fetch('tasks').find { |task| task['name'].to_s == name }
    raise "the first play of #{PLAYBOOK_FILE} has no task named #{name.inspect}" if guard.nil?

    that = TaskExpressions.condition_list(guard.dig('ansible.builtin.assert', 'that'))
    result = TaskExpressions.evaluate(
      variables: variables(**).merge('ansible_play_hosts_all' => play_hosts, 'groups' => GROUPS),
      facts: [], conditions: { 'guard' => that }
    )
    result['conditions']['guard']
  end

  # Each bootstrap guard case: the flag, the play hosts, and whether it passes.
  GUARD_CASES = [
    [true, PRODUCTION_HOSTS, false],
    [true, QA_HOSTS + [PRODUCTION_HOSTS.last], false],
    [true, QA_HOSTS, true],
    [false, PRODUCTION_HOSTS, true]
  ].freeze
  # Each audit guard case: the bootstrap flag, the audit flag, the play hosts,
  # and whether the assert passes.
  AUDIT_GUARD_CASES = [
    [true, true, QA_HOSTS, true],
    [false, true, QA_HOSTS, false],
    [true, true, QA_HOSTS + [PRODUCTION_HOSTS.last], false],
    [false, false, PRODUCTION_HOSTS, true],
    [true, false, QA_HOSTS, true]
  ].freeze
end

RSpec.describe TackLedgerBootstrap do
  it 'runs both guards once on the controller as the first tasks of the first play', :aggregate_failures do
    play = described_class.first_play
    first_tasks = play.fetch('tasks').first(2)

    expect(play['hosts']).to eq('tack_all')
    expect(first_tasks.map { |task| task['name'] }).to eq([TackLedgerBootstrap::GUARD_TASK_NAME, TackLedgerBootstrap::AUDIT_GUARD_TASK_NAME])
    expect(first_tasks.map { |task| [task['run_once'], task['delegate_to']] }).to all(eq([true, 'localhost']))
  end

  it 'refuses either flag with a production guest and the audit flag without the bootstrap flag', :aggregate_failures do
    TackLedgerBootstrap::GUARD_CASES.each do |bootstrap, play_hosts, passes|
      result = described_class.guard_passes(TackLedgerBootstrap::GUARD_TASK_NAME, play_hosts: play_hosts, bootstrap: bootstrap)
      expect(result).to be(passes), "bootstrap=#{bootstrap} hosts=#{play_hosts}"
    end
    TackLedgerBootstrap::AUDIT_GUARD_CASES.each do |bootstrap, audit, play_hosts, passes|
      result = described_class.guard_passes(TackLedgerBootstrap::AUDIT_GUARD_TASK_NAME,
                                            play_hosts: play_hosts, bootstrap: bootstrap, audit: audit)
      expect(result).to be(passes), "bootstrap=#{bootstrap} audit=#{audit} hosts=#{play_hosts}"
    end
  end

  it 'runs audit-bootstrap on the first node only in run A', :aggregate_failures do
    run_a = described_class.rendered(bootstrap: true, audit: true, node: 'yb1')
    run_b = described_class.rendered(bootstrap: true, audit: false, node: 'yb1')

    expect(run_a).to eq('command' => TackLedgerBootstrap::START_WAIT + TackLedgerBootstrap::AUDIT_BOOTSTRAP, 'rotation_wait' => false)
    expect(run_b).to eq('command' => TackLedgerBootstrap::START_WAIT, 'rotation_wait' => false)
  end

  it 'waits on later nodes with the node position as the counts and the replicas on the last', :aggregate_failures do
    [true, false].each do |audit|
      second = described_class.rendered(bootstrap: true, audit: audit, node: 'yb2', rotated: true)
      third = described_class.rendered(bootstrap: true, audit: audit, node: 'yb3')

      expect(second['command']).to eq(TackLedgerBootstrap::START_WAIT + described_class.bootstrap_wait(2)), "audit=#{audit}"
      expect(third['command']).to eq(TackLedgerBootstrap::START_WAIT + described_class.bootstrap_wait(3, replicas: true)),
                                  "audit=#{audit}"
      expect([second['rotation_wait'], third['rotation_wait']]).to eq([false, false]), "audit=#{audit}"
    end
  end

  it 'keeps the prepare, start, and wait order and the rotation wait on every other run', :aggregate_failures do
    expect(described_class.rendered(bootstrap: false)).to eq('command' => TackLedgerBootstrap::FULL_SEQUENCE, 'rotation_wait' => false)
    expect(described_class.rendered(bootstrap: false, rotated: true)).to eq(
      'command' => TackLedgerBootstrap::PREPARE + TackLedgerBootstrap::START_AFTER_PREPARE, 'rotation_wait' => true
    )
  end
end
