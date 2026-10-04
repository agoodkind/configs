# frozen_string_literal: true

require 'digest'
require 'json'
require 'tmpdir'
require 'yaml'
require_relative 'ansible_render'
require_relative 'command_runner'
require_relative 'task_expressions'

# Helpers for the operator identity specs: the flags tack_all.yml renders,
# the shared identity guard, and real deploy-tack runs in check mode.
module TackOpsIdentityFlags
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'deploy-tack.yml')
  GROUP_VARS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'inventory', 'group_vars', 'tack_all.yml')
  GUARD_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks', 'tack-ops-identity-guard.yml')
  GUARD = 'Refuse an operator identity the ops commands cannot record truthfully'
  GUARD_MESSAGE = 'The rendered identity flags list each flag once'
  AGENT_SERVICE = 'claude-rowan'
  IDENTITY_VARS = %w[tack_ops_agent_run deploy_operator_email deploy_operator_name deploy_operator_id
                     tack_ops_identity_flags].freeze
  OPERATOR_NAMESPACE = '0a6f7572-cafe-dead-beef-000000000004'
  ACCOUNTABLE_EMAIL = 'accountable@example.invalid'
  HUMAN_EMAIL = 'human@example.invalid'
  SESSION = 'a5fcf5d2-43ec-4bbe-b412-0452ecc45408'
  COMMIT = '66ef50f670fd1cd6a5f4dfe8f877af30ed6c2b51'

  module_function

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

  # deploy-tack and tack-ops import the guard from its own task file.
  def guard
    YAML.safe_load_file(GUARD_FILE).find { |task| task['name'] == GUARD }
  end

  # The one host is a search guest in an environment with search off. The
  # first play groups it into tack_deploy_skipped, and every later play
  # selects no host. A run on this inventory changes no guest.
  DEPLOY_INVENTORY = "[tack_all]\nguard-test ansible_connection=local tack_cluster_role=search tack_search_enabled=false\n"
  DEPLOY_TIMEOUT_SECONDS = 120

  # The agent shell markers the guard reads from the control machine
  # environment. A nil value removes the variable from the child process.
  def shell_environment(agent:)
    { 'CLAUDECODE' => (agent ? '1' : nil), 'CODEX_THREAD_ID' => nil }
  end

  # Runs a real playbook, deploy-tack by default, in check mode against that
  # inventory, with the agent shell markers set or removed, and returns its
  # output and exit status.
  def run_deploy(identity, agent:, playbook: PLAYBOOK_FILE, inventory_text: DEPLOY_INVENTORY)
    Dir.mktmpdir('identity-guard') do |directory|
      inventory = File.join(directory, 'inventory.ini')
      File.write(inventory, inventory_text)
      password = File.join(directory, 'vault-password')
      File.write(password, AnsibleRender::VAULT_PASSWORD_PLACEHOLDER, perm: AnsibleRender::SECRET_FILE_MODE)
      argv = [AnsibleRender::PLAYBOOK_COMMAND, '--check', '--inventory', inventory, playbook,
              '--extra-vars', JSON.generate(identity)]
      environment = shell_environment(agent: agent).merge(AnsibleRender::VAULT_PASSWORD_ENV => password)
      CommandRunner.run(environment, argv, chdir: AnsibleRender::ANSIBLE_DIRECTORY, timeout_seconds: DEPLOY_TIMEOUT_SECONDS)
    end
  end

  # The operator email a run renders: the accountable email on an agent run,
  # the control machine's email otherwise.
  def operator_email(service)
    service.empty? ? HUMAN_EMAIL : ACCOUNTABLE_EMAIL
  end

  # The flag string tack_all.yml renders for a service and session.
  def rendered_flags(service:, session:)
    email = operator_email(service)
    identity = "--operator-id #{operator_id(email)} --operator-email #{email}"
    return "#{identity} --operator-name \"Human Operator\" --deploy-commit #{COMMIT}" if service.empty?

    "--operator-service #{service} --operator-session #{session} #{identity} --deploy-commit #{COMMIT}"
  end

  # The two ledger bootstrap guards run before the identity check and read
  # tack_ledger_bootstrap and tack_ledger_audit_bootstrap, which this
  # inventory does not set. The temporary inventory loads no group vars, so
  # the run passes the identity variables tack_all.yml would render.
  def identity(service:, session:, overrides: {})
    email = operator_email(service)
    { 'tack_ops_agent_service' => service, 'tack_ops_agent_session' => session,
      'tack_ops_accountable_email' => (service.empty? ? '' : ACCOUNTABLE_EMAIL),
      'deploy_operator_email' => email, 'deploy_operator_id' => operator_id(email), 'tack_commit' => COMMIT,
      'tack_ops_identity_flags' => rendered_flags(service: service, session: session),
      'tack_ledger_bootstrap' => false, 'tack_ledger_audit_bootstrap' => false }.merge(overrides)
  end

  # Evaluates the guard with the agent shell marker set or removed in this
  # process; replace sets 'accountable', 'email', or 'flags'.
  def guard_passes?(service: '', session: '', agent_run: false, replace: {})
    conditions = TaskExpressions.condition_list(guard.dig('ansible.builtin.assert', 'that'))
    variables = identity(service: service, session: session)
    variables['tack_ops_accountable_email'] = replace['accountable'] if replace.key?('accountable')
    if replace.key?('email')
      variables['deploy_operator_email'] = replace['email']
      variables['tack_ops_identity_flags'] = variables['tack_ops_identity_flags'].sub(operator_email(service), replace['email'])
    end
    variables['tack_ops_identity_flags'] = replace['flags'] if replace.key?('flags')
    with_environment(shell_environment(agent: agent_run)) do
      TaskExpressions.evaluate(variables: variables, facts: [], conditions: { 'guard' => conditions })
                     .dig('conditions', 'guard')
    end
  end

  def with_environment(environment)
    saved = environment.keys.to_h { |key| [key, ENV.fetch(key, nil)] }
    environment.each { |key, value| ENV[key] = value }
    yield
  ensure
    saved.each { |key, value| ENV[key] = value }
  end

  # Rendered agent flags with one part replaced, by the gap each leaves.
  def replaced_flags
    good = rendered_flags(service: AGENT_SERVICE, session: SESSION)
    { 'no service' => good.sub("--operator-service #{AGENT_SERVICE} ", ''),
      'no session' => good.sub("--operator-session #{SESSION} ", ''),
      'another commit' => good.sub(COMMIT, 'main'),
      'a second operator id' => "#{good} --operator-id 00000000-0000-5000-8000-000000000000" }
  end
end
