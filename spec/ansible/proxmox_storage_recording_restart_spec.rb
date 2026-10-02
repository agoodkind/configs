# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# These examples evaluate the iostat recorder restart condition with
# ansible-core against registered loop results in different orders.
module ProxmoxStorageRecordingRestart
  TASKS_FILE = File.join(AnsibleRender::ANSIBLE_DIRECTORY, 'playbooks', 'tasks', 'proxmox-storage-recording.yml')
  RESTART = 'Restart the zpool iostat recorder after its unit changed'
  IOSTAT = 'services/storage-recording-iostat.service'

  module_function

  # Each pair is a loop item and whether its template task changed.
  def restarts?(items)
    results = items.map { |item, changed| { 'item' => item, 'changed' => changed } }
    task = YAML.safe_load_file(TASKS_FILE).find { |candidate| candidate['name'] == RESTART }
    variables = { 'storage_recording_units' => { 'results' => results } }
    TaskExpressions.evaluate(variables: variables, facts: [],
                             conditions: { 'restart' => TaskExpressions.condition_list(task['when']) })
                   .dig('conditions', 'restart')
  end
end

RSpec.describe ProxmoxStorageRecordingRestart do
  it 'restarts when the iostat unit changed, at any list position', :aggregate_failures do
    expect(described_class.restarts?([[ProxmoxStorageRecordingRestart::IOSTAT, true],
                                      ['services/storage-recording-objset.service', false]])).to be(true)
    expect(described_class.restarts?([['services/storage-recording-objset.service', false],
                                      [ProxmoxStorageRecordingRestart::IOSTAT, true]])).to be(true)
  end

  it 'does not restart when only another unit changed', :aggregate_failures do
    expect(described_class.restarts?([[ProxmoxStorageRecordingRestart::IOSTAT, false],
                                      ['services/storage-recording-objset.service', true]])).to be(false)
    expect(described_class.restarts?([['services/storage-recording-objset.service', true],
                                      [ProxmoxStorageRecordingRestart::IOSTAT, false]])).to be(false)
  end
end
