# frozen_string_literal: true

require 'digest'
require 'yaml'
require_relative '../support/tack_search_inventory'
require_relative '../support/task_expressions'

# These examples evaluate the expressions that decide when a deploy writes the
# security files into a running OpenSearch member and that build each request
# body from the installed files.
module TackSearchSecurity
  TASKS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks', 'tack-search-security.yml')
  ROLES_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'tack', 'opensearch-roles.yml')
  WRITE_BLOCK = 'Write the OpenSearch security files into the running member'
  ROLE_WRITE = 'Write each OpenSearch role into the security index'
  USER_WRITE = 'Write each OpenSearch user into the security index'
  CHECKSUMS = { users: 'users-sha1', roles: 'roles-sha1', mapping: 'mapping-sha1' }.freeze

  module_function

  def block
    YAML.safe_load_file(TASKS_FILE).find { |task| task['name'] == WRITE_BLOCK }
  end

  def block_task(name)
    block.fetch('block').find { |task| task['name'] == name }
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
  def writes?(record)
    variables = installed_files.merge('tack_search_security_record' => { 'stat' => record })
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
    expect(described_class.writes?('exists' => false)).to be(true)
  end

  it 'skips the write when the record matches the installed files' do
    expect(described_class.writes?(described_class.applied_record)).to be(false)
  end

  it 'writes the security files when an installed file changed' do
    record = described_class.applied_record.merge('checksum' => Digest::SHA1.hexdigest('users-sha1 roles-sha1 old-mapping'))

    expect(described_class.writes?(record)).to be(true)
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
