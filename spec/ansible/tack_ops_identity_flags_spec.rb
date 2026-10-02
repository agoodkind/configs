# frozen_string_literal: true

require 'digest'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# These examples render the deploy-tack operator identity flags with
# ansible-core for a human deploy and an agent deploy, and evaluate the check
# that the first play of deploy-tack runs before any guest changes.
module TackOpsIdentityFlags
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'deploy-tack.yml')
  GROUP_VARS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', 'group_vars', 'tack_all.yml')
  FIRST_PLAY = 'Select the Tack deploy targets'
  GUARD = 'Refuse an operator identity the ops commands cannot record truthfully'
  IDENTITY_VARS = %w[tack_ops_agent_run deploy_operator_email deploy_operator_name deploy_operator_id
                     tack_ops_identity_flags].freeze
  OPERATOR_NAMESPACE = '0a6f7572-cafe-dead-beef-000000000004'
  ACCOUNTABLE_EMAIL = 'accountable@example.invalid'
  HUMAN_EMAIL = 'human@example.invalid'
  SESSION = 'a5fcf5d2-43ec-4bbe-b412-0452ecc45408'
  COMMIT = '66ef50f670fd1cd6a5f4dfe8f877af30ed6c2b51'

  module_function

  def plays
    YAML.safe_load_file(PLAYBOOK_FILE)
  end

  # The version 5 UUID that internal/cli/operator_git.go derives from an email.
  def operator_id(email)
    namespace = [OPERATOR_NAMESPACE.delete('-')].pack('H*')
    bytes = Digest::SHA1.digest(namespace + email.downcase).bytes.first(16)
    bytes[6] = (bytes[6] & 0x0f) | 0x50
    bytes[8] = (bytes[8] & 0x3f) | 0x80
    hex = bytes.pack('C*').unpack1('H*')
    [hex[0, 8], hex[8, 4], hex[12, 4], hex[16, 4], hex[20, 12]].join('-')
  end

  # Renders the flags from the group vars. The test variables replace the
  # operator name and any override; the rendered values call no git config
  # lookup.
  def flags(service:, session:, overrides: {})
    group_vars = YAML.safe_load_file(GROUP_VARS_FILE).slice(*IDENTITY_VARS).except(*overrides.keys)
    variables = { 'tack_commit' => COMMIT, 'deploy_operator_name' => 'Human Operator',
                  'tack_ops_agent_service' => service, 'tack_ops_agent_session' => session,
                  'tack_ops_accountable_email' => ACCOUNTABLE_EMAIL }.merge(overrides)
    template_vars = group_vars.except('deploy_operator_name')
    rendered = TaskExpressions.evaluate(
      variables: variables, facts: [],
      renders: [TaskExpressions.render_task({ 'vars' => template_vars }, { 'flags' => '{{ tack_ops_identity_flags }}' })]
    )
    rendered.fetch('renders').first.fetch('flags').split
  end

  def guard
    plays.find { |play| play['name'] == FIRST_PLAY }.fetch('tasks').find { |task| task['name'] == GUARD }
  end

  def guard_passes?(service: '', session: '', agent_run: false, accountable: '', email: HUMAN_EMAIL)
    conditions = TaskExpressions.condition_list(guard.dig('ansible.builtin.assert', 'that'))
    variables = { 'tack_ops_agent_service' => service, 'tack_ops_agent_session' => session,
                  'tack_ops_agent_run' => agent_run, 'tack_ops_accountable_email' => accountable,
                  'deploy_operator_email' => email }
    TaskExpressions.evaluate(variables: variables, facts: [], conditions: { 'guard' => conditions })
                   .dig('conditions', 'guard')
  end
end

RSpec.describe TackOpsIdentityFlags do
  let(:accountable_id) { described_class.operator_id(TackOpsIdentityFlags::ACCOUNTABLE_EMAIL) }
  let(:human_id) { described_class.operator_id(TackOpsIdentityFlags::HUMAN_EMAIL) }

  it 'records the human operator when no agent service is set' do
    flags = described_class.flags(service: '', session: '',
                                  overrides: { 'deploy_operator_email' => TackOpsIdentityFlags::HUMAN_EMAIL })

    expect(flags).to eq(
      ['--operator-id', human_id, '--operator-email', TackOpsIdentityFlags::HUMAN_EMAIL,
       '--operator-name', '"Human', 'Operator"', '--deploy-commit', TackOpsIdentityFlags::COMMIT]
    )
  end

  it 'records the agent service and session as the actor and the accountable person as on_behalf_of' do
    expect(described_class.flags(service: 'claude-rowan', session: TackOpsIdentityFlags::SESSION)).to eq(
      ['--operator-service', 'claude-rowan', '--operator-session', TackOpsIdentityFlags::SESSION,
       '--operator-id', accountable_id, '--operator-email', TackOpsIdentityFlags::ACCOUNTABLE_EMAIL,
       '--deploy-commit', TackOpsIdentityFlags::COMMIT]
    )
  end

  it 'derives the operator ID the Tack command line derives for the same email' do
    expect(described_class.operator_id('goodkindalex@gmail.com')).to eq('b8cfe465-3681-5eb2-89da-0b228a7c8d0f')
  end

  it 'checks the identity in the first play, before any play changes a guest', :aggregate_failures do
    expect(described_class.plays.first['name']).to eq(TackOpsIdentityFlags::FIRST_PLAY)
    expect(described_class.plays.first['tasks'].first['name']).to eq(TackOpsIdentityFlags::GUARD)
    expect(described_class.guard).to include('run_once' => true, 'delegate_to' => 'localhost')
  end

  it 'refuses an agent shell without an agent service and accepts an agent shell with one', :aggregate_failures do
    expect(described_class.guard_passes?(agent_run: true)).to be(false)
    expect(described_class.guard_passes?(service: 'claude-rowan', session: 's1', agent_run: true)).to be(true)
    expect(described_class.guard_passes?).to be(true)
  end

  it 'refuses a service without a session, a session without a service, and an invalid service name', :aggregate_failures do
    expect(described_class.guard_passes?(service: 'claude-rowan', agent_run: true)).to be(false)
    expect(described_class.guard_passes?(session: 's1')).to be(false)
    expect(described_class.guard_passes?(service: 'Claude Rowan', session: 's1', agent_run: true)).to be(false)
  end

  it 'refuses a session that is not one token of letters, digits, and hyphens', :aggregate_failures do
    expect(described_class.guard_passes?(service: 'claude-rowan', session: TackOpsIdentityFlags::SESSION)).to be(true)
    ['a b', 'a;reboot', "a\nreboot", 'a$(id)', 'a' * 65].each do |session|
      expect(described_class.guard_passes?(service: 'claude-rowan', session: session)).to be(false)
    end
  end

  it 'refuses an accountable email on a human deploy' do
    expect(described_class.guard_passes?(accountable: TackOpsIdentityFlags::ACCOUNTABLE_EMAIL)).to be(false)
  end

  it 'refuses an empty or malformed operator email', :aggregate_failures do
    ['', 'no-at-sign', 'two@at@signs', "human@example.invalid\n"].each do |email|
      expect(described_class.guard_passes?(email: email)).to be(false)
    end
  end
end
