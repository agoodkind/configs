# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# These examples evaluate the storage recording systemd task conditions with
# ansible-core: the recorded set of changed unit files, the iostat start and
# restart, and the timer enable loop, in a check run and in a real run.
module ProxmoxStorageRecordingSystemd
  TASKS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks', 'proxmox-storage-recording.yml')
  RECORD = 'Record the storage recording unit files this run installs or changes'
  START = 'Start the per-minute zpool iostat recorder'
  RESTART = 'Restart the zpool iostat recorder after its unit changed'
  TIMERS = 'Enable the storage recording timers'
  IOSTAT = 'services/storage-recording-iostat.service'
  UNIT_ITEMS = [IOSTAT, 'services/storage-recording-objset.service', 'services/storage-recording-histogram.service',
                'services/storage-recording-fdb-restarts.service', 'timers/storage-recording-fdb-restarts.timer'].freeze
  STATIC_TIMER_ITEMS = %w[storage-recording-objset.timer storage-recording-histogram.timer].freeze
  TIMER_NAMES = %w[storage-recording-objset.timer storage-recording-histogram.timer
                   storage-recording-fdb-restarts.timer].freeze

  module_function

  def task(name)
    YAML.safe_load_file(TASKS_FILE).find { |candidate| candidate['name'] == name }
  end

  def results(items, changed)
    { 'changed' => items.any? { |item| changed.include?(item) },
      'results' => items.map { |item| { 'item' => item, 'changed' => changed.include?(item) } } }
  end

  # Evaluates every systemd task condition for one run. changed lists the unit
  # and timer file items with a changed template or copy result.
  def conditions(check_mode:, changed:)
    variables = { 'ansible_check_mode' => check_mode,
                  'storage_recording_units' => results(UNIT_ITEMS, changed),
                  'storage_recording_static_timers' => results(STATIC_TIMER_ITEMS, changed) }
    facts = [TaskExpressions.fact_task(task(RECORD))]
    iostat = TaskExpressions.evaluate(
      variables: variables, facts: facts,
      conditions: { 'start' => TaskExpressions.condition_list(task(START)['when']),
                    'restart' => TaskExpressions.condition_list(task(RESTART)['when']) }
    ).fetch('conditions')
    timer_conditions = TaskExpressions.condition_list(task(TIMERS)['when'])
    timers = TIMER_NAMES.to_h do |name|
      verdict = TaskExpressions.evaluate(variables: variables.merge('item' => name), facts: facts,
                                         conditions: { 'enable' => timer_conditions })
      [name, verdict.dig('conditions', 'enable')]
    end
    iostat.merge('timers' => timers)
  end
end

RSpec.describe ProxmoxStorageRecordingSystemd do
  let(:all_files) { ProxmoxStorageRecordingSystemd::UNIT_ITEMS + ProxmoxStorageRecordingSystemd::STATIC_TIMER_ITEMS }
  let(:timers_skipped) { ProxmoxStorageRecordingSystemd::TIMER_NAMES.to_h { |name| [name, false] } }
  let(:timers_run) { ProxmoxStorageRecordingSystemd::TIMER_NAMES.to_h { |name| [name, true] } }

  it 'skips every systemd task in a check run that only previews the unit files' do
    expect(described_class.conditions(check_mode: true, changed: all_files))
      .to eq('start' => false, 'restart' => false, 'timers' => timers_skipped)
  end

  it 'starts, restarts, and enables every unit in a real run that installs the unit files' do
    expect(described_class.conditions(check_mode: false, changed: all_files))
      .to eq('start' => true, 'restart' => true, 'timers' => timers_run)
  end

  it 'starts and enables installed units in a check run and skips the restart' do
    expect(described_class.conditions(check_mode: true, changed: []))
      .to eq('start' => true, 'restart' => false, 'timers' => timers_run)
  end

  it 'skips only the timer with a previewed file in a check run' do
    result = described_class.conditions(check_mode: true, changed: ['storage-recording-histogram.timer'])

    expect(result.fetch('timers')).to eq('storage-recording-objset.timer' => true, 'storage-recording-histogram.timer' => false,
                                         'storage-recording-fdb-restarts.timer' => true)
  end

  it 'restarts in a real run only when the iostat unit changed', :aggregate_failures do
    expect(described_class.conditions(check_mode: false, changed: [ProxmoxStorageRecordingSystemd::IOSTAT])
      .fetch('restart')).to be(true)
    expect(described_class.conditions(check_mode: false, changed: ['services/storage-recording-objset.service'])
      .fetch('restart')).to be(false)
  end
end
