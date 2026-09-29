# frozen_string_literal: true

require 'json'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# The deploy runs the Tack metadata name index backfill (TACK-542) on the owner
# guest before the app starts. A Tack image without the command lets the deploy
# finish and fails every later search index build. Remove this file together
# with the backfill tasks, which Tack removes by 2026-11-30.
module TackMetadataNameIndex
  PLAYBOOK_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'deploy-tack.yml')
  TASKS_DIRECTORY = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks')
  GUARD_INCLUDE = 'tasks/tack-search-metadata-name-index.yml'
  GUARD_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', GUARD_INCLUDE)
  ASSUME_TASK = 'Assume the metadata name index backfill has not run'
  PROVISION_TASK = 'Provision (idempotent first boot; configure fresh fdb only where allowed, migrate, seed audit roles, seed product)'
  LIST_TASK = 'List the backfills in the pinned Tack image'
  PRESENT_TASK = 'Record whether the pinned Tack image includes the metadata name index backfill'
  BRANCH_TASK = 'Report whether this deploy runs the metadata name index backfill'
  STOP_TASK = 'Stop the deploy when search is on and the pinned Tack image lacks the metadata name index backfill'
  RUN_TASK = 'Write the missing metadata name index entries'
  COUNTS_TASK = 'Report the metadata name index entries the backfill wrote'
  READY_TASK = 'Record whether search may build an index on this deploy'
  APP_START_TASK = 'Start Tack services (second pass; fdb configured, app and audit-consumer come up)'
  SEARCH_MEMBER_TASK = 'Run the OpenSearch member'
  BACKFILL_TASKS = [LIST_TASK, PRESENT_TASK, BRANCH_TASK, STOP_TASK, RUN_TASK, COUNTS_TASK, READY_TASK].freeze
  # A task that runs one of these commands builds or rebuilds a search index.
  SEARCH_BUILD_COMMAND = /\bops search (?:provision|reindex|access-rollout)\b/
  COMMAND_MODULES = %w[ansible.builtin.command ansible.builtin.shell].freeze
  INCLUDE_MODULE = 'ansible.builtin.include_tasks'
  NESTED_KEYS = %w[block rescue always].freeze

  # `ops backfill --help` in cobra's format from an image with the command, and
  # from an image older than it. An image without a backfill group prints the
  # ops listing, which also names no metadata name index backfill.
  LISTING_WITH_COMMAND = <<~HELP
    One-time jobs, each deleted by its removal day

    Usage:
      tack ops backfill [command]

    Available Commands:
      once-metadata-name-index Write the missing type-key and property-name index entries
      once-search-projections  Write the explicit search projection metadata
  HELP
  LISTING_WITHOUT_COMMAND = <<~HELP
    One-time jobs, each deleted by its removal day

    Usage:
      tack ops backfill [command]

    Available Commands:
      once-search-projections Write the explicit search projection metadata
  HELP

  module_function

  def tasks_in(tasks)
    tasks.flat_map { |task| [task] + NESTED_KEYS.flat_map { |key| tasks_in(task[key] || []) } }
  end

  def playbook_tasks
    YAML.safe_load_file(PLAYBOOK_FILE).flat_map { |play| tasks_in(play['tasks'] || []) }
  end

  def task_named(tasks, name)
    task = tasks.find { |candidate| candidate['name'] == name }
    raise "#{PLAYBOOK_FILE} has no task named #{name.inspect}" if task.nil?

    task
  end

  def position(tasks, name)
    tasks.index(task_named(tasks, name))
  end

  # The block of the owner guest that contains the provision task.
  def owner_block(tasks)
    tasks.find { |task| (task['block'] || []).any? { |child| child['name'] == PROVISION_TASK } }
  end

  def search_build?(task)
    COMMAND_MODULES.any? { |name| task[name].to_s.match?(SEARCH_BUILD_COMMAND) }
  end

  def guard_include?(task)
    task.nil? ? false : task[INCLUDE_MODULE] == GUARD_INCLUDE
  end

  # Every search build task in the playbook and the tack task files, paired with
  # the task directly before it.
  def search_builds
    files = [PLAYBOOK_FILE] + Dir.glob(File.join(TASKS_DIRECTORY, 'tack-*.yml'))
    files.flat_map do |file|
      loaded = YAML.safe_load_file(file)
      tasks = file == PLAYBOOK_FILE ? playbook_tasks : tasks_in(loaded)
      tasks.each_index.select { |index| search_build?(tasks[index]) }.map do |index|
        { file: file, task: tasks[index], previous: index.zero? ? nil : tasks[index - 1] }
      end
    end
  end

  # A registered ansible.builtin.command result of the backfill with
  # --output json, in Tack's result envelope.
  def backfill_result(node_types_missing, property_definitions_missing)
    envelope = {
      '_meta' => { 'trace_id' => '0af7651916cd43dd8448eb211c80319c' },
      'result' => {
        'command' => 'ops.backfill.once-metadata-name-index',
        'dry_run' => false,
        'result' => {
          'node_types' => { 'scanned' => 12, 'missing' => node_types_missing },
          'property_definitions' => { 'scanned' => 40, 'missing' => property_definitions_missing }
        }
      }
    }
    TaskExpressions.command_result(0, JSON.pretty_generate(envelope), '')
  end

  # Evaluates the gate tasks as the deploy runs them. A skipped run task leaves
  # a registered result without stdout.
  def gate(listing, backfill = { 'changed' => false, 'skipped' => true }, search_enabled: false)
    tasks = playbook_tasks
    branch = task_named(tasks, BRANCH_TASK)
    stop = task_named(tasks, STOP_TASK)
    counts = task_named(tasks, COUNTS_TASK)
    run = task_named(tasks, RUN_TASK)
    guard = YAML.safe_load_file(GUARD_FILE).first
    conditions = {
      'run' => TaskExpressions.condition_list(run['when']),
      'counts' => TaskExpressions.condition_list(counts['when']),
      'guard' => TaskExpressions.condition_list(guard.dig('ansible.builtin.assert', 'that')),
      'stop' => TaskExpressions.condition_list(stop['when'])
    }
    present = TaskExpressions.condition_list(run['when'])
    conditions['changed'] = present + TaskExpressions.condition_list(run['changed_when'])
    TaskExpressions.evaluate(
      variables: {
        'tack_commit' => '9f3c2ab', 'tack_backfill_commands' => TaskExpressions.command_result(0, listing, ''),
        'tack_metadata_name_index_backfill' => backfill, 'tack_search_enabled' => search_enabled
      },
      facts: [ASSUME_TASK, PRESENT_TASK, READY_TASK].map { |name| TaskExpressions.fact_task(task_named(tasks, name)) },
      conditions: conditions,
      renders: [
        TaskExpressions.render_task(branch, 'msg' => branch.dig('ansible.builtin.debug', 'msg')),
        TaskExpressions.render_task(stop, 'msg' => stop.dig('ansible.builtin.fail', 'msg'))
      ]
    )
  end

  def rendered_counts(backfill)
    counts = task_named(playbook_tasks, COUNTS_TASK)
    render = TaskExpressions.render_task(counts, 'msg' => counts.dig('ansible.builtin.debug', 'msg'))
    TaskExpressions.evaluate(
      variables: { 'tack_metadata_name_index_backfill' => backfill }, facts: [], renders: [render]
    )['renders'][0]['msg']
  end
end

RSpec.describe TackMetadataNameIndex do
  describe 'the place of the backfill in the deploy' do
    let(:tasks) { described_class.playbook_tasks }

    it 'runs the backfill on the owner guest after provision and before the app and every search step', :aggregate_failures do
      block = described_class.owner_block(tasks)
      names = block['block'].map { |task| task['name'] }
      order = [TackMetadataNameIndex::PROVISION_TASK, *TackMetadataNameIndex::BACKFILL_TASKS, TackMetadataNameIndex::APP_START_TASK]

      positions = order.map { |name| names.index(name) }

      expect(block['when']).to eq('tack_provision_owner')
      expect(positions).to all(be_an(Integer))
      expect(positions).to eq(positions.sort)
      expect(described_class.position(tasks, TackMetadataNameIndex::ASSUME_TASK)).to be < described_class.position(tasks, TackMetadataNameIndex::LIST_TASK)
      expect(described_class.position(tasks, TackMetadataNameIndex::READY_TASK)).to be < described_class.position(tasks, TackMetadataNameIndex::SEARCH_MEMBER_TASK)
    end

    it 'puts the guard directly before every search index build' do
      described_class.search_builds.each do |build|
        expect(described_class.guard_include?(build[:previous])).to be(true), "#{build[:file]}: #{build[:task]['name']} has no guard before it"
      end
    end
  end

  describe 'the gate on the pinned Tack image' do
    it 'runs the backfill and allows search index builds when the image lists the command', :aggregate_failures do
      result = described_class.gate(TackMetadataNameIndex::LISTING_WITH_COMMAND, described_class.backfill_result(3, 7), search_enabled: true)

      expect(result['facts']).to eq('tack_metadata_name_index_backfill_present' => true, 'tack_metadata_name_index_ready' => true)
      expect(result['conditions']).to eq('run' => true, 'counts' => true, 'guard' => true, 'stop' => false, 'changed' => true)
      expect(result['renders'][0]['msg']).to eq(
        'The Tack image for 9f3c2ab includes ops backfill once-metadata-name-index. This deploy runs it with --execute.'
      )
    end

    it 'reports no change on a rerun with no missing entry' do
      result = described_class.gate(TackMetadataNameIndex::LISTING_WITH_COMMAND, described_class.backfill_result(0, 0))

      expect(result['conditions']).to include('run' => true, 'guard' => true, 'changed' => false)
    end

    it 'logs the written counts from the command output' do
      message = described_class.rendered_counts(described_class.backfill_result(3, 7))

      expect(message).to eq(
        'The backfill scanned 12 node types and wrote 3 type-key entries. It scanned 40 property definitions and wrote 7 property-name entries.'
      )
    end

    it 'finishes the deploy, says so, and fails search index builds when search is off and the image lacks the command', :aggregate_failures do
      result = described_class.gate(TackMetadataNameIndex::LISTING_WITHOUT_COMMAND, search_enabled: false)

      expect(result['facts']).to eq('tack_metadata_name_index_backfill_present' => false, 'tack_metadata_name_index_ready' => false)
      expect(result['conditions']).to eq('run' => false, 'counts' => false, 'guard' => false, 'stop' => false, 'changed' => false)
      expect(result['renders'][0]['msg']).to eq(
        'The Tack image for 9f3c2ab does not include ops backfill once-metadata-name-index. The metadata name index ' \
        'backfill did not run, and every later search provision or rebuild step of this deploy fails.'
      )
    end

    it 'stops the deploy before the app starts when search is on and the image lacks the command', :aggregate_failures do
      result = described_class.gate(TackMetadataNameIndex::LISTING_WITHOUT_COMMAND, search_enabled: true)
      names = described_class.owner_block(described_class.playbook_tasks)['block'].map { |task| task['name'] }

      expect(result['conditions']).to include('run' => false, 'stop' => true)
      expect(names.index(TackMetadataNameIndex::STOP_TASK)).to be < names.index(TackMetadataNameIndex::APP_START_TASK)
      expect(result['renders'][1]['msg']).to eq(
        'The Tack image for 9f3c2ab does not include ops backfill once-metadata-name-index. The metadata name index ' \
        'backfill did not run, and every later search provision or rebuild step of this deploy fails. Search is on for ' \
        'this environment. The deploy stops before the app starts. Deploy a Tack commit that includes the command.'
      )
    end

    it 'fails a search index build on a guest where the backfill did not run' do
      guard = YAML.safe_load_file(TackMetadataNameIndex::GUARD_FILE).first
      assume = described_class.task_named(described_class.playbook_tasks, TackMetadataNameIndex::ASSUME_TASK)
      conditions = { 'guard' => TaskExpressions.condition_list(guard.dig('ansible.builtin.assert', 'that')) }

      result = TaskExpressions.evaluate(variables: {}, facts: [TaskExpressions.fact_task(assume)], conditions: conditions)

      expect(result['conditions']['guard']).to be(false)
    end
  end
end
