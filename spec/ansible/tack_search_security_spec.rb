# frozen_string_literal: true

require 'digest'
require 'yaml'
require_relative '../support/tack_search_inventory'
require_relative '../support/task_expressions'

# These examples evaluate the expressions that decide when a deploy writes the
# security files into a running OpenSearch member and that build each request
# body from the installed files.
module TackSearchSecurity
  TASKS_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks')
  TASKS_FILE = File.join(TASKS_DIRECTORY, 'tack-search-security.yml')
  NODE_TASKS_FILE = File.join(TASKS_DIRECTORY, 'tack-search-node.yml')
  ROLES_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'tack', 'opensearch-roles.yml')
  WRITE_BLOCK = 'Write the OpenSearch security files into the running member'
  ROLE_WRITE = 'Write each OpenSearch role into the security index'
  USER_WRITE = 'Write each OpenSearch user into the security index'
  CHECKSUMS = { users: 'users-sha1', roles: 'roles-sha1', mapping: 'mapping-sha1' }.freeze
  # A task that names one of these variables or module options reads,
  # writes, or passes the admin private key.
  KEY_REFERENCES = %w[tack_search_admin_private_key tack_search_admin_key_file client_key].freeze
  BLOCK_SECTIONS = %w[block rescue always].freeze
  INHERITED_KEYWORDS = %w[delegate_to no_log].freeze

  module_function

  def block
    YAML.safe_load_file(TASKS_FILE).find { |task| task['name'] == WRITE_BLOCK }
  end

  def block_task(name)
    tasks(TASKS_FILE).find { |task| task['name'] == name }
  end

  # Every task of a task file with the delegate_to and no_log keywords it
  # inherits from its enclosing blocks.
  def tasks(file)
    flatten(YAML.safe_load_file(file), {})
  end

  def flatten(entries, inherited)
    entries.flat_map do |entry|
      keywords = inherited.merge(entry.slice(*INHERITED_KEYWORDS))
      sections = BLOCK_SECTIONS.filter_map { |section| entry[section] }
      next [entry.merge(keywords)] if sections.empty?

      sections.flat_map { |section| flatten(section, keywords) }
    end
  end

  def key_tasks(file)
    tasks(file).select do |task|
      text = YAML.dump(task)
      KEY_REFERENCES.any? { |reference| text.include?(reference) }
    end
  end

  def installed_files
    {
      'tack_search_users_file' => { 'checksum' => CHECKSUMS.fetch(:users) },
      'tack_search_roles_file' => { 'checksum' => CHECKSUMS.fetch(:roles) },
      'tack_search_roles_mapping_file' => { 'checksum' => CHECKSUMS.fetch(:mapping) }
    }
  end

  # The record stores the checksums that the last write applied. stat reports
  # the SHA-1 of the record content.
  def writes?(record, check_mode: false)
    variables = installed_files.merge(
      'tack_search_security_record' => { 'stat' => record }, 'ansible_check_mode' => check_mode
    )
    result = TaskExpressions.evaluate(
      variables: variables,
      facts: [{ 'when' => [], 'vars' => {}, 'set_fact' => block.fetch('vars') }],
      conditions: { 'write' => TaskExpressions.condition_list(block['when']) }
    )
    result.dig('conditions', 'write')
  end

  def applied_record
    { 'exists' => true, 'checksum' => Digest::SHA1.hexdigest(CHECKSUMS.values.join(' ')) }
  end

  # The loop of a write task, rendered against the slurped content of one
  # installed file.
  def entries(task_name, register_name, content)
    variables = { register_name => { 'content' => [content].pack('m0') } }
    result = TaskExpressions.evaluate(
      variables: variables, facts: [],
      renders: [TaskExpressions.render_task({}, { 'entries' => block_task(task_name).fetch('loop') })]
    )
    result.fetch('renders').first.fetch('entries')
  end
end

RSpec.describe TackSearchSecurity do
  it 'writes the security files when the guest has no record' do
    expect(described_class.writes?({ 'exists' => false })).to be(true)
  end

  it 'skips the write when the record matches the installed files' do
    expect(described_class.writes?(described_class.applied_record)).to be(false)
  end

  it 'writes the security files when an installed file changed' do
    record = described_class.applied_record.merge('checksum' => Digest::SHA1.hexdigest('users-sha1 roles-sha1 old-mapping'))

    expect(described_class.writes?(record)).to be(true)
  end

  it 'sends no security write in check mode' do
    expect(described_class.writes?({ 'exists' => false }, check_mode: true)).to be(false)
  end

  it 'handles the admin private key only on the deploy host and never logs it', :aggregate_failures do
    key_tasks = described_class.key_tasks(TackSearchSecurity::TASKS_FILE)

    expect(key_tasks.map { |task| task['name'] }).to include(
      'Write the admin private key on the deploy host', TackSearchSecurity::ROLE_WRITE, TackSearchSecurity::USER_WRITE
    )
    key_tasks.each do |task|
      expect(task['delegate_to']).to eq('localhost'), "#{task['name']} runs on the guest"
      expect(task['no_log']).to be(true), "#{task['name']} logs its content"
    end
    expect(described_class.key_tasks(TackSearchSecurity::NODE_TASKS_FILE)).to eq([])
  end

  it 'writes each role from the installed role file and skips the file metadata' do
    roles = described_class.entries(TackSearchSecurity::ROLE_WRITE, 'tack_search_roles_read', File.read(TackSearchSecurity::ROLES_FILE))

    expect(roles.map { |entry| entry.fetch('key') }).to eq(['tack_search'])
    expect(roles.first.dig('value', 'reserved')).to be(true)
    expect(roles.first.dig('value', 'cluster_permissions')).to include(
      'cluster:monitor/state', 'cluster:admin/opensearch/ml/deploy_model_on_nodes', 'cluster:admin/opensearch/ml/stats/nodes'
    )
  end

  it 'writes the rendered search user with its password hash' do
    rendered = TackSearchInventory.rendered(:qa)
    users_file = rendered.file(rendered.member_host('tack_search1_suburban'), 'users')

    users = described_class.entries(TackSearchSecurity::USER_WRITE, 'tack_search_users_read', users_file)

    expect(users).to eq(
      [{
        'key' => 'render-only-vault_tack_qa_search_username',
        'value' => {
          'hash' => 'render-only-vault_tack_qa_search_password_hash', 'reserved' => true,
          'description' => 'The Tack application and the Tack operator commands use this user.'
        }
      }]
    )
  end
end
