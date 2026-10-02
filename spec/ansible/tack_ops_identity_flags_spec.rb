# frozen_string_literal: true

require 'digest'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# These examples render the deploy-tack operator identity flags with
# ansible-core for a human deploy and an agent deploy, and evaluate the check
# that refuses an agent deploy without the agent service identity.
module TackOpsIdentityFlags
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'deploy-tack.yml')
  PLAY = 'Provision Tack via Docker Compose'
  GUARD = 'Refuse an agent deploy without the agent service identity'
  OPERATOR_NAMESPACE = '0a6f7572-cafe-dead-beef-000000000004'
  ACCOUNTABLE_EMAIL = 'accountable@example.invalid'
  COMMIT = '66ef50f670fd1cd6a5f4dfe8f877af30ed6c2b51'

  module_function

  def play
    YAML.safe_load_file(PLAYBOOK_FILE).find { |candidate| candidate['name'] == PLAY }
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

  # Renders the flags with the play vars. A set accountable email skips the
  # git config lookup; the test variables supply the operator name.
  def flags(service:, session:)
    play_vars = play.fetch('vars').except('deploy_operator_name', 'tack_ops_agent_service',
                                          'tack_ops_agent_session', 'tack_ops_accountable_email')
    variables = { 'tack_commit' => COMMIT, 'deploy_operator_name' => 'Accountable Person',
                  'tack_ops_agent_service' => service, 'tack_ops_agent_session' => session,
                  'tack_ops_accountable_email' => ACCOUNTABLE_EMAIL }
    rendered = TaskExpressions.evaluate(
      variables: variables, facts: [],
      renders: [TaskExpressions.render_task({ 'vars' => play_vars }, { 'flags' => '{{ tack_ops_identity_flags }}' })]
    )
    rendered.fetch('renders').first.fetch('flags').split
  end

  def guard_passes?(service:, session:, agent_run:)
    task = play.fetch('tasks').find { |candidate| candidate['name'] == GUARD }
    conditions = TaskExpressions.condition_list(task.dig('ansible.builtin.assert', 'that'))
    variables = { 'tack_ops_agent_service' => service, 'tack_ops_agent_session' => session,
                  'tack_ops_agent_run' => agent_run }
    TaskExpressions.evaluate(variables: variables, facts: [], conditions: { 'guard' => conditions })
                   .dig('conditions', 'guard')
  end
end

RSpec.describe TackOpsIdentityFlags do
  let(:accountable_id) { described_class.operator_id(TackOpsIdentityFlags::ACCOUNTABLE_EMAIL) }

  it 'records the human operator when no agent service is set' do
    expect(described_class.flags(service: '', session: '')).to eq(
      ['--operator-id', accountable_id, '--operator-email', TackOpsIdentityFlags::ACCOUNTABLE_EMAIL,
       '--operator-name', '"Accountable', 'Person"', '--deploy-commit', TackOpsIdentityFlags::COMMIT]
    )
  end

  it 'records the agent service and session as the actor and the accountable person as on_behalf_of' do
    expect(described_class.flags(service: 'claude-rowan', session: 'a5fcf5d2-43ec-4bbe-b412-0452ecc45408')).to eq(
      ['--operator-service', 'claude-rowan', '--operator-session', 'a5fcf5d2-43ec-4bbe-b412-0452ecc45408',
       '--operator-id', accountable_id, '--operator-email', TackOpsIdentityFlags::ACCOUNTABLE_EMAIL,
       '--deploy-commit', TackOpsIdentityFlags::COMMIT]
    )
  end

  it 'derives the operator ID the Tack command line derives for the same email' do
    expect(described_class.operator_id('goodkindalex@gmail.com')).to eq('b8cfe465-3681-5eb2-89da-0b228a7c8d0f')
  end

  it 'refuses an agent shell without an agent service and accepts an agent shell with one', :aggregate_failures do
    expect(described_class.guard_passes?(service: '', session: '', agent_run: true)).to be(false)
    expect(described_class.guard_passes?(service: 'claude-rowan', session: 's1', agent_run: true)).to be(true)
    expect(described_class.guard_passes?(service: '', session: '', agent_run: false)).to be(true)
  end

  it 'refuses a service without a session, a session without a service, and an invalid service name', :aggregate_failures do
    expect(described_class.guard_passes?(service: 'claude-rowan', session: '', agent_run: true)).to be(false)
    expect(described_class.guard_passes?(service: '', session: 's1', agent_run: false)).to be(false)
    expect(described_class.guard_passes?(service: 'Claude Rowan', session: 's1', agent_run: true)).to be(false)
  end
end
