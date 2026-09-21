# frozen_string_literal: true

require 'tmpdir'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# A guest seeded with an assembled cluster file joins a different cluster. These
# checks prove the deploy copies the live file and the environment repeats it.
module TackStoreClusterFile
  PLAYBOOK_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'deploy-tack.yml')
  GROUP_VARS_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'inventory', 'group_vars', 'tack_all.yml')
  ENV_TEMPLATE_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'tack', 'tack.env.j2')
  RECORD_TASK_NAME = 'Record the live product store cluster file for this guest'
  READ_TASK_NAME = "Read this environment's live product store cluster file"
  NO_STORE_TASK_NAME = 'Fail when this guest has no cluster file and no guest of this run runs the product store'
  NO_FILE_TASK_NAME = 'Fail when the guest running the product store has no cluster file'
  SEED_TASK_NAME = 'Seed the product store cluster file where this guest has none'
  START_STORES_TASK_NAME = 'Start the other stores and wait until they are healthy'
  COPY_MODULE = 'ansible.builtin.copy'
  CONTENTS_SETTING = 'FDB_CLUSTER_FILE_CONTENTS'
  PROCESS_HOSTS_VAR = 'tack_store_process_hosts'
  SOURCE_VAR = 'tack_store_cluster_file_source'

  # Every verdict a case asserts. A task name maps to the label the result uses.
  CONDITION_TASKS = {
    'read' => READ_TASK_NAME,
    'seed' => SEED_TASK_NAME,
    'no_store' => NO_STORE_TASK_NAME,
    'no_file' => NO_FILE_TASK_NAME
  }.freeze

  # A cluster file as a cluster writes it after a coordinator move: the
  # description, the generated key, and the coordinator the cluster runs.
  LIVE_CLUSTER_FILE = 'docker:Q7vRnT4pXz2mBw8sLd6h@[3d06:bad:b01:210::217]:4500'
  OWNER_GUEST = 'tack-qa'
  DATA_GUESTS = %w[tack-data1 tack-data2 tack-data3].freeze

  # A registered result as Ansible leaves it where the task did not run. It
  # includes no stat member, and a condition list that evaluated one would fail
  # the run with an undefined error.
  SKIPPED_TASK = { 'changed' => false, 'skipped' => true }.freeze

  # The per-environment and per-guest values the environment template reads on a
  # guest that runs no store process and no backup timers. Every other value
  # comes from the group_vars file the render play loads.
  ENVIRONMENT_VARS = {
    'tack_store_host' => '3d06:bad:b01:210::217',
    'tack_yugabyte_password' => 'render-only-ledger-login',
    'tack_meili_master_key' => 'render-only-search-login',
    'tack_audit_writer_password' => 'render-only-writer-login',
    'tack_audit_reader_password' => 'render-only-reader-login',
    'tack_audit_redactor_password' => 'render-only-redactor-login',
    'tack_audit_operator_password' => 'render-only-operator-login',
    'tack_app_password' => 'render-only-app-login',
    'tack_audit_valid_signers' => ['ed25519:0000000000000000'],
    'tack_kafka_cluster_id' => 'renderOnlyClusterId00A',
    'tack_datagen_allow_target' => 'qa',
    'tack_docker_v6_subnet' => '3d06:bad:b01:210:1::/96',
    'tack_docker_v6_gateway' => '3d06:bad:b01:210:1::1',
    'tack_backup_enabled' => false
  }.freeze

  module_function

  # Every task of every play, including the tasks of a block.
  def tasks_in(tasks)
    tasks.flat_map { |task| [task] + tasks_in(task['block'] || []) + tasks_in(task['always'] || []) }
  end

  def playbook_tasks
    plays = YAML.safe_load_file(PLAYBOOK_FILE)
    plays.flat_map { |play| tasks_in(play['tasks'] || []) }
  end

  def task_named(tasks, name)
    task = tasks.find { |candidate| candidate['name'].to_s == name }
    raise "#{PLAYBOOK_FILE} has no task named #{name.inspect}" if task.nil?

    task
  end

  # The registered result of the stat that looks for the live cluster file.
  def stat_result(exists)
    { 'changed' => false, 'failed' => false, 'stat' => { 'exists' => exists } }
  end

  # The registered result of the slurp that reads it, base64 as slurp returns.
  def read_result(contents)
    { 'changed' => false, 'failed' => false, 'content' => [contents].pack('m0'), 'encoding' => 'base64' }
  end

  # own is the stat of this guest's own cluster file; a guest with none is the
  # default, the case every seed decision is about.
  def seed_variables(stat:, read: nil, source: OWNER_GUEST, bootstrap: false, seed: true, own: stat_result(false))
    {
      'tack_store_seed_cluster_file' => seed,
      'tack_store_bootstrap' => bootstrap,
      'tack_store_live_cluster_file' => '',
      SOURCE_VAR => source,
      'tack_store_own_cluster_file' => own,
      'tack_store_cluster_file_stat' => stat,
      'tack_store_cluster_file_read' => read
    }.compact
  end

  # One guest's own declarations, the two the source selection reads.
  def guest_vars(name, owner:, store:)
    { 'inventory_hostname' => name, 'tack_provision_owner' => owner, 'tack_store_node_present' => store }
  end

  # An inventory of one owner guest and three data guests, with the store
  # processes the migration has started so far.
  def play_hostvars(data_guests_run_store:)
    guests = { OWNER_GUEST => guest_vars(OWNER_GUEST, owner: true, store: false) }
    DATA_GUESTS.each do |name|
      guests[name] = guest_vars(name, owner: false, store: data_guests_run_store)
    end
    guests
  end

  # The when list of each task above, read from the playbook.
  def task_conditions(tasks)
    conditions = {}
    CONDITION_TASKS.each do |label, name|
      conditions[label] = TaskExpressions.condition_list(task_named(tasks, name)['when'])
    end
    conditions
  end

  # Evaluates the real source selection from tack_all.yml against one
  # inventory, the way a play resolves it.
  def selected_source(hostvars, legacy_present:)
    group_vars = YAML.safe_load_file(GROUP_VARS_FILE, aliases: true)
    render = {
      'vars' => {
        'ansible_play_hosts_all' => hostvars.keys,
        'hostvars' => hostvars,
        'tack_store_legacy_node_present' => legacy_present,
        PROCESS_HOSTS_VAR => group_vars.fetch(PROCESS_HOSTS_VAR)
      },
      'templates' => { 'source' => group_vars.fetch(SOURCE_VAR) }
    }
    TaskExpressions.evaluate(variables: {}, facts: [], renders: [render])['renders'][0]['source']
  end

  # Renders the environment file for a guest from the cluster file the run read
  # off the guest running the store.
  def rendered_environment(live_cluster_file)
    Dir.mktmpdir('tack-env') do |output_directory|
      output_file = File.join(output_directory, '.env')
      AnsibleRender.render(
        inventory: 'localhost,',
        playbook: 'render_tack_env.yml',
        extra_vars: ENVIRONMENT_VARS.merge(
          'tack_store_live_cluster_file' => live_cluster_file,
          'group_vars_file' => GROUP_VARS_FILE,
          'template_file' => ENV_TEMPLATE_FILE,
          'output_file' => output_file
        )
      )
      File.read(output_file)
    end
  end

  def rendered_environment_setting(live_cluster_file, setting)
    setting_line = rendered_environment(live_cluster_file).lines(chomp: true).find do |line|
      line.start_with?("#{setting}=")
    end
    setting_line.to_s.delete_prefix("#{setting}=")
  end

  def rendered_start_stores_command
    task = task_named(playbook_tasks, START_STORES_TASK_NAME)
    render = TaskExpressions.render_task(
      task, 'command' => task.dig('ansible.builtin.command', 'cmd')
    )
    result = TaskExpressions.evaluate(
      variables: { 'tack_ledger_legacy_node_present' => false }, facts: [], renders: [render]
    )
    result['renders'][0]['command']
  end
end

RSpec.describe TackStoreClusterFile do
  describe 'the cluster file the deploy seeds' do
    before(:all) do
      tasks = described_class.playbook_tasks
      @record_fact = TaskExpressions.fact_task(described_class.task_named(tasks, TackStoreClusterFile::RECORD_TASK_NAME))
      seed_task = described_class.task_named(tasks, TackStoreClusterFile::SEED_TASK_NAME)
      @seed_render = TaskExpressions.render_task(
        seed_task, 'content' => seed_task.dig(TackStoreClusterFile::COPY_MODULE, 'content')
      )
      @conditions = described_class.task_conditions(tasks)
    end

    def verdicts(variables, renders: [])
      TaskExpressions.evaluate(
        variables: variables, facts: [@record_fact], conditions: @conditions, renders: renders
      )
    end

    it 'writes the live bytes the run read off the guest running the store', :aggregate_failures do
      live = TackStoreClusterFile::LIVE_CLUSTER_FILE
      variables = described_class.seed_variables(
        stat: described_class.stat_result(true), read: described_class.read_result("#{live}\n")
      )

      result = verdicts(variables, renders: [@seed_render])

      expect(result['facts']['tack_store_live_cluster_file']).to eq(live),
                                                                 'the run must keep the generated key of the live file'
      expect(result['renders'][0]['content']).to eq("#{live}\n")
      expect(result['conditions']).to eq('read' => true, 'seed' => true, 'no_store' => false, 'no_file' => false)
    end

    it 'stops the run when the guest running the store has no cluster file' do
      result = verdicts(described_class.seed_variables(stat: described_class.stat_result(false)))

      expect(result['conditions']).to eq('read' => false, 'seed' => false, 'no_store' => false, 'no_file' => true)
    end

    it 'stops the run when no guest of the run runs a store process' do
      variables = described_class.seed_variables(stat: TackStoreClusterFile::SKIPPED_TASK, source: '')

      result = verdicts(variables)

      expect(result['conditions']).to eq('read' => false, 'seed' => false, 'no_store' => true, 'no_file' => false)
    end

    # A deploy limited to guests that already have their cluster file, the
    # owner guest alone after the data guests took the store, has no source and
    # nothing to seed.
    it 'seeds nothing and stops nothing on a guest that already has its cluster file when the run has no source' do
      variables = described_class.seed_variables(
        stat: TackStoreClusterFile::SKIPPED_TASK, source: '', own: described_class.stat_result(true)
      )

      result = verdicts(variables)

      expect(result['conditions']).to eq('read' => false, 'seed' => false, 'no_store' => false, 'no_file' => false)
    end

    it 'seeds nothing on a from-empty environment that declares the bootstrap' do
      variables = described_class.seed_variables(stat: TackStoreClusterFile::SKIPPED_TASK, source: '', bootstrap: true)

      result = verdicts(variables)

      expect(result['conditions']).to eq('read' => false, 'seed' => false, 'no_store' => false, 'no_file' => false)
    end

    # Production leaves the seed off. Ansible skips the stat on that run, and
    # the registered result then includes no stat at all. Every condition list
    # reads the flag first and stops on it.
    it 'skips the seed where the environment has not turned it on' do
      result = verdicts(described_class.seed_variables(stat: TackStoreClusterFile::SKIPPED_TASK, seed: false))

      expect(result['conditions']).to eq('read' => false, 'seed' => false, 'no_store' => false, 'no_file' => false)
    end
  end

  # The guest a run reads is derived from each guest's own declarations. The
  # source follows the migration rather than naming one guest forever.
  describe 'the guest the run reads the cluster file from' do
    it 'is the owner guest while its original store process runs' do
      hostvars = described_class.play_hostvars(data_guests_run_store: true)

      source = described_class.selected_source(hostvars, legacy_present: true)

      expect(source).to eq(TackStoreClusterFile::OWNER_GUEST)
    end

    it 'is the first data guest once the owner guest retires its process' do
      hostvars = described_class.play_hostvars(data_guests_run_store: true)

      source = described_class.selected_source(hostvars, legacy_present: false)

      expect(source).to eq(TackStoreClusterFile::DATA_GUESTS.first)
    end

    it 'is empty where no guest of the run runs a store process' do
      hostvars = described_class.play_hostvars(data_guests_run_store: false)

      source = described_class.selected_source(hostvars, legacy_present: false)

      expect(source).to eq('')
    end
  end

  describe 'the cluster file in the rendered environment' do
    it 'repeats the live bytes for the container' do
      live = TackStoreClusterFile::LIVE_CLUSTER_FILE

      rendered = described_class.rendered_environment_setting(live, TackStoreClusterFile::CONTENTS_SETTING)

      expect(rendered).to eq(live)
    end

    it 'stays empty where the run read no cluster file' do
      rendered = described_class.rendered_environment_setting('', TackStoreClusterFile::CONTENTS_SETTING)

      expect(rendered).to eq('')
    end
  end

  describe 'search removal in the rendered deployment' do
    # The tack stack file declares MEILI_MASTER_KEY required on three
    # services, so a deploy without it fails at `docker compose pull`. This
    # expectation flips to absence in the change that stops tack declaring it.
    it 'renders the search credential the stack file requires' do
      environment = described_class.rendered_environment('')

      expect(environment).to include('MEILI_MASTER_KEY=')
    end

    it 'starts the remaining stores without a search service or a workflow engine' do
      command = described_class.rendered_start_stores_command

      expect(command.split).to eq(
        %w[docker compose up -d --wait --wait-timeout 300 kafka clickhouse]
      )
    end
  end
end
