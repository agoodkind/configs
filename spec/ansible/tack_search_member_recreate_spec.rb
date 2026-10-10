# frozen_string_literal: true

require 'json'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/command_runner'
require_relative '../support/tack_search_outage_compose'

# The regression targets the duplicate recreation observed during a QA search guest deploy.
module TackSearchMemberRecreate
  PLAYBOOKS_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks')
  PLAYBOOK_FILE = File.join(PLAYBOOKS_DIRECTORY, 'deploy-tack.yml')
  TASKS_DIRECTORY = File.join(PLAYBOOKS_DIRECTORY, 'tasks')
  NODE_TASKS_FILE = File.join(TASKS_DIRECTORY, 'tack-search-node.yml')
  IMPORT_KEY = 'ansible.builtin.import_tasks'
  FETCH_KEY = 'ansible.builtin.get_url'
  COPY_KEY = 'ansible.builtin.copy'
  ORDER_KEYS = ['ansible.builtin.meta', 'ansible.builtin.command'].freeze
  HOST = 'search-member'
  INVENTORY_TEXT = "#{HOST} ansible_connection=local ansible_become=false\n".freeze
  CHANGED_COMPOSE_FILE = 'changed-compose.yaml'
  CHANGED_ENVIRONMENT = { 'TACK_SEARCH_DEFINITION' => 'changed' }.freeze
  CONTAINER_SUFFIX = "-#{TackSearchOutageCompose::SERVICE}-1".freeze
  # The Compose project creates its initial container before the playbook runs.
  INITIAL_CREATIONS = 1
  RUN_TIMEOUT_SECONDS = 120

  module_function

  def ordered_tasks(file, changed_compose)
    YAML.safe_load_file(file).flat_map do |task|
      next ordered_tasks(File.join(TASKS_DIRECTORY, task.fetch(IMPORT_KEY)), changed_compose) if task.key?(IMPORT_KEY)
      next [definition_change(task, changed_compose)] if task.key?(FETCH_KEY)
      next [task] if ORDER_KEYS.any? { |key| task.key?(key) }

      []
    end
  end

  def definition_change(task, changed_compose)
    destination = File.join('{{ tack_install_dir }}', TackSearchOutageCompose::COMPOSE_FILE)
    task.slice('name', 'notify').merge(COPY_KEY => { 'src' => changed_compose, 'dest' => destination, 'mode' => '0644' })
  end

  def handlers(tasks)
    notified = tasks.flat_map { |task| Array(task['notify']) }.uniq
    declared = YAML.safe_load_file(PLAYBOOK_FILE).flat_map { |play| play['handlers'] || [] }
    selected = declared.select { |handler| notified.include?(handler['name']) }
    raise "#{PLAYBOOK_FILE} has no handler named #{notified.inspect}" if selected.empty?

    selected
  end

  def play(directory, project)
    changed_compose = File.join(directory, CHANGED_COMPOSE_FILE)
    service = TackSearchOutageCompose.definition(project)
    service.fetch('services').fetch(TackSearchOutageCompose::SERVICE)['environment'] = CHANGED_ENVIRONMENT
    File.write(changed_compose, YAML.dump(service))
    tasks = ordered_tasks(NODE_TASKS_FILE, changed_compose)
    { 'hosts' => HOST, 'gather_facts' => false, 'tasks' => tasks, 'handlers' => handlers(tasks) }
  end

  def run_play(directory, play)
    playbook = File.join(directory, 'playbook.yml')
    File.write(playbook, YAML.dump([play]))
    inventory = File.join(directory, 'inventory.ini')
    File.write(inventory, INVENTORY_TEXT)
    password = File.join(directory, 'vault-password')
    File.write(password, AnsibleRender::VAULT_PASSWORD_PLACEHOLDER, perm: AnsibleRender::SECRET_FILE_MODE)
    variables = { 'tack_install_dir' => directory, 'tack_cluster_role' => 'search' }
    argv = [AnsibleRender::PLAYBOOK_COMMAND, '--inventory', inventory, playbook, '--extra-vars', JSON.generate(variables)]
    CommandRunner.run({ AnsibleRender::VAULT_PASSWORD_ENV => password }, argv,
                      chdir: AnsibleRender::ANSIBLE_DIRECTORY, timeout_seconds: RUN_TIMEOUT_SECONDS)
  end

  def recreations(directory, project)
    arguments = ['events', '--since', '0', '--until', Time.now.to_i.to_s, '--filter', 'type=container',
                 '--filter', 'event=create', '--filter', "label=com.docker.compose.project=#{project}",
                 '--format', '{{.Actor.ID}}']
    TackSearchOutageCompose.docker(directory, arguments).lines.size - INITIAL_CREATIONS
  end
end

RSpec.describe TackSearchMemberRecreate do
  it 'recreates the OpenSearch member once after a Compose definition change', :aggregate_failures do
    TackSearchOutageCompose.with_project do |directory, container|
      project = container.delete_suffix(TackSearchMemberRecreate::CONTAINER_SUFFIX)
      play = described_class.play(directory, project)
      result = described_class.run_play(directory, play)

      expect(result.exit_status.success?).to be(true), result.output
      play.fetch('handlers').each do |handler|
        expect(result.output).to include("RUNNING HANDLER [#{handler.fetch('name')}]")
      end
      recreations = described_class.recreations(directory, project)
      expect(recreations).to eq(1), "#{recreations} container recreations\n#{result.output}"
      expect(TackSearchOutageCompose.running?(directory, container)).to be(true)
    end
  end
end
