# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'
require_relative '../support/tack_ops_identity'

# tack-ops runs one reviewed Tack ops command on one QA guest with the rendered
# operator identity. These examples run the real playbook through its identity
# guard and evaluate the real playbook text: the request check, the task
# order, and the rendered command lines.
module TackOpsPlaybook
  PLAYBOOKS = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks')
  PLAYBOOK_FILE = File.join(PLAYBOOKS, 'tack-ops.yml')
  REQUEST_TASK = 'Refuse a Tack ops request outside the reviewed commands, guests, or arguments'
  VERIFY_TASK = 'Verify that the guest runs the Tack images for tack_commit'
  DRY_RUN_TASK = 'Print the dry run of the Tack ops command'
  EXECUTE_TASK = 'Run the Tack ops command with --execute'
  EXECUTE_OUTPUT_TASK = 'Show the output of the --execute run'
  QA_HOSTS = %w[tack-qa tack-data1-suburban tack-data2-suburban tack-data3-suburban].freeze
  PRODUCTION_HOSTS = %w[tack tack-data1 tack-data2 tack-data3].freeze
  GROUPS = { 'tack_qa_all' => QA_HOSTS, 'tack_prod_all' => PRODUCTION_HOSTS }.freeze
  COMMIT = '66ef50f670fd1cd6a5f4dfe8f877af30ed6c2b51'
  SERVER_DIGEST = "sha256:#{'a' * 64}".freeze
  CONSUMER_DIGEST = "sha256:#{'b' * 64}".freeze
  AGENT_FLAGS = "--operator-service claude-luna --operator-session s1 --operator-id 0a6f --operator-email a@b.c --deploy-commit #{COMMIT}".freeze

  module_function

  def plays(file = PLAYBOOK_FILE)
    YAML.safe_load_file(file, aliases: true)
  end

  def task(play, name)
    found = play.fetch('tasks').find { |candidate| candidate['name'] == name }
    raise "#{PLAYBOOK_FILE} has no task named #{name.inspect}" if found.nil?

    found
  end

  def request(**overrides)
    { 'tack_commit' => COMMIT, 'tack_ops_command' => 'ops search verify', 'tack_ops_args' => [],
      'tack_server_digest' => SERVER_DIGEST, 'tack_audit_consumer_digest' => CONSUMER_DIGEST,
      'ansible_play_hosts_all' => ['tack-qa'], 'groups' => GROUPS }.merge(overrides)
  end

  # Whether the request check passes for variables, from the real assert.
  def request_passes(**overrides)
    that = TaskExpressions.condition_list(task(plays.first, REQUEST_TASK).dig('ansible.builtin.assert', 'that'))
    TaskExpressions.evaluate(variables: request(**overrides), facts: [], conditions: { 'request' => that })
                   .dig('conditions', 'request')
  end

  # The command line a task in the second play renders, split on whitespace.
  def command_line(name, command:, args: [])
    rendered = TaskExpressions.render_task({}, 'cmd' => task(plays[1], name).dig('ansible.builtin.command', 'cmd'))
    variables = request('tack_ops_command' => command, 'tack_ops_args' => args).merge('tack_ops_identity_flags' => AGENT_FLAGS)
    TaskExpressions.evaluate(variables: variables, facts: [], renders: [rendered])['renders'][0]['cmd'].split
  end

  # Whether the --execute output task runs for a registered --execute result,
  # and the message it prints when it runs.
  def execute_output(registered)
    output_task = task(plays[1], EXECUTE_OUTPUT_TASK)
    conditions = { 'runs' => TaskExpressions.condition_list(output_task['when']) }
    variables = { 'tack_ops_execute_run' => registered }
    runs = TaskExpressions.evaluate(variables: variables, facts: [], conditions: conditions).dig('conditions', 'runs')
    return [runs, nil] unless runs

    rendered = TaskExpressions.render_task({}, 'msg' => output_task.dig('ansible.builtin.debug', 'msg'))
    [runs, TaskExpressions.evaluate(variables: variables, facts: [], renders: [rendered])['renders'][0]['msg']]
  end

  REFUSED_REQUESTS = {
    'a command outside the allowlist' => { 'tack_ops_command' => 'ops db sql' },
    'an argument that adds a shell operator' => { 'tack_ops_args' => ['--mode=full;id'] },
    'a positional argument' => { 'tack_ops_args' => ['SELECT'] },
    'an argument that adds --execute' => { 'tack_ops_args' => ['--execute'] },
    'an argument that sets an identity flag' => { 'tack_ops_args' => ['--operator-id=x'] },
    'a production guest' => { 'ansible_play_hosts_all' => ['tack'] },
    'two guests' => { 'ansible_play_hosts_all' => %w[tack-qa tack-data1-suburban] },
    'a short commit' => { 'tack_commit' => '66ef50f' },
    'an empty command' => { 'tack_ops_command' => '' }
  }.freeze
end

RSpec.describe TackOpsPlaybook do
  it 'stops a tack-ops run from an agent shell without a service before the request check', :aggregate_failures do
    identity = TackOpsIdentityFlags.identity(service: '', session: '')
    result = TackOpsIdentityFlags.run_deploy(identity, agent: true, playbook: TackOpsPlaybook::PLAYBOOK_FILE)

    expect(result.exit_status.success?).to be(false)
    expect(result.output).to include(TackOpsIdentityFlags::GUARD_MESSAGE)
    expect(result.output).not_to include("TASK [#{TackOpsPlaybook::REQUEST_TASK}]")
  end

  it 'accepts an allowlisted command with flag arguments on one QA guest' do
    expect(described_class.request_passes('tack_ops_args' => ['--scale=small', '--commit'],
                                          'tack_ops_command' => 'ops qa datagen seed')).to be(true)
  end

  it 'refuses every request outside the reviewed commands, guests, and arguments', :aggregate_failures do
    TackOpsPlaybook::REFUSED_REQUESTS.each do |label, overrides|
      expect(described_class.request_passes(**overrides.transform_keys(&:to_s))).to be(false), label
    end
  end

  it 'verifies the deployed images first and runs the dry run before the --execute task', :aggregate_failures do
    names = described_class.plays[1].fetch('tasks').map { |task| task['name'] }
    execute = described_class.task(described_class.plays[1], TackOpsPlaybook::EXECUTE_TASK)
    dry_run = described_class.task(described_class.plays[1], TackOpsPlaybook::DRY_RUN_TASK)

    expect(names.first).to eq(TackOpsPlaybook::VERIFY_TASK)
    expect(names.index(TackOpsPlaybook::DRY_RUN_TASK)).to be < names.index(TackOpsPlaybook::EXECUTE_TASK)
    expect(dry_run['when']).to be_nil
    expect(execute['when']).to eq('tack_ops_execute | bool')
  end

  it 'renders the agent identity on the dry run and the --execute run', :aggregate_failures do
    flags = TackOpsPlaybook::AGENT_FLAGS.split
    dry_run = described_class.command_line(TackOpsPlaybook::DRY_RUN_TASK, command: 'ops qa datagen seed', args: ['--scale=small'])
    execute = described_class.command_line(TackOpsPlaybook::EXECUTE_TASK, command: 'ops deploy verify')

    expect(dry_run).to eq(%w[docker compose run --rm app ops qa datagen seed --scale=small] + flags)
    expect(execute).to eq(%w[docker compose run --rm tack-ops ops deploy verify] + flags + ['--execute'])
  end

  it 'accepts ops audit prove-schema-guard and runs it in the tack-ops service', :aggregate_failures do
    flags = TackOpsPlaybook::AGENT_FLAGS.split
    command = 'ops audit prove-schema-guard'
    dry_run = described_class.command_line(TackOpsPlaybook::DRY_RUN_TASK, command: command)
    execute = described_class.command_line(TackOpsPlaybook::EXECUTE_TASK, command: command)

    expect(described_class.request_passes('tack_ops_command' => command)).to be(true)
    expect(dry_run).to eq(%w[docker compose run --rm tack-ops ops audit prove-schema-guard] + flags)
    expect(execute).to eq(%w[docker compose run --rm tack-ops ops audit prove-schema-guard] + flags + ['--execute'])
  end

  it 'accepts audit query with a verb and an RFC3339 window and runs it in the app service', :aggregate_failures do
    args = ['--org=0190a3c4-7d2e-7c1a-9f3b-2b6e8d4c1a5f', '--action=ops.audit_schema_guard_proof',
            '--oldest=2026-10-06T10:00:00Z', '--latest=2026-10-06T11:00:00Z']
    execute = described_class.command_line(TackOpsPlaybook::EXECUTE_TASK, command: 'audit query', args: args)

    expect(described_class.request_passes('tack_ops_command' => 'audit query', 'tack_ops_args' => args)).to be(true)
    expect(execute).to eq(%w[docker compose run --rm app audit query] + args + TackOpsPlaybook::AGENT_FLAGS.split + ['--execute'])
  end

  it 'prints the --execute output only after the --execute task ran', :aggregate_failures do
    report = '{"command":"ops.audit_schema_guard_proof"}'
    ran = TaskExpressions.command_result(0, report, '').merge('stdout_lines' => [report])

    expect(described_class.execute_output(ran)).to eq([true, [report]])
    expect(described_class.execute_output({ 'changed' => false, 'skipped' => true })).to eq([false, nil])
  end
end
