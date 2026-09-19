# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# deploy-proxmox forces handlers after a failure, and the hypervisor deploy's
# config template notifies the drain and bridge restart handlers. After the deploy
# renames a new opnsensectl over the installed binary, a handler restart before
# `opnsensectl install` writes the units would start the new binary from the old
# unit, which exits without --config until systemd stops retrying. These checks
# evaluate the handlers' when lists with ansible-core's templar after the facts
# the real task file sets up to each point in that block.
module OpnsenseHostHandlerGuard
  PLAY_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'deploy-proxmox.yml')
  TASK_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'tasks', 'mwan-opnsense-host-deploy.yml')
  HANDLER_NAMES = ['Restart mwan-opnsense-drain', 'Restart mwan-opnsense-host'].freeze
  MOVE_BLOCK_NAME = 'Move the hypervisor onto the incoming opnsensectl'
  RENAME_TASK_NAME = 'Rename the incoming opnsensectl over the installed binary'
  INSTALL_TASK_NAME = 'Install and enable the drain and bridge units with opnsensectl'

  module_function

  def play
    YAML.safe_load_file(PLAY_FILE).first
  end

  def handler_conditions
    conditions = {}
    HANDLER_NAMES.each do |name|
      handler = play['handlers'].find { |task| task['name'] == name }
      raise "#{PLAY_FILE} has no handler #{name.inspect}" if handler.nil?

      conditions[name] = TaskExpressions.condition_list(handler['when'])
    end
    conditions
  end

  def move_block
    block = YAML.safe_load_file(TASK_FILE).find { |task| task['name'] == MOVE_BLOCK_NAME }
    raise "#{TASK_FILE} has no block #{MOVE_BLOCK_NAME.inspect}" if block.nil?

    block
  end

  # The set_fact tasks in the move block that run before the task named stop_at,
  # each carrying the block's own when list ahead of its own.
  def facts_before(stop_at)
    block = move_block
    block_when = TaskExpressions.condition_list(block['when'])
    facts = []
    block['block'].each do |task|
      return facts if task['name'] == stop_at

      next if task[TaskExpressions::SET_FACT_KEY].nil?

      fact = TaskExpressions.fact_task(task)
      fact['when'] = block_when + fact['when']
      facts << fact
    end
    raise "#{TASK_FILE} block #{MOVE_BLOCK_NAME.inspect} has no task #{stop_at.inspect}" unless stop_at.nil?

    facts
  end

  # The play vars the handler conditions read, as literals. Other play vars are
  # templates over playbook_dir, which only a real play defines.
  def play_variables(conditions)
    variables = { 'ansible_check_mode' => false }
    play['vars'].each do |name, value|
      next unless conditions.values.flatten.any? { |condition| condition.include?(name) }

      variables[name] = value
    end
    variables
  end

  def verdicts(facts)
    conditions = handler_conditions
    result = TaskExpressions.evaluate(variables: play_variables(conditions), facts: facts, conditions: conditions)
    result['conditions'] || {}
  end
end

RSpec.describe OpnsenseHostHandlerGuard do
  it 'runs the restart handlers when the deploy never reached the rename' do
    verdicts = described_class.verdicts([])

    OpnsenseHostHandlerGuard::HANDLER_NAMES.each do |name|
      expect(verdicts[name]).to be(true), "#{name} would be skipped on a run that changed no binary"
    end
  end

  it 'skips the restart handlers when install fails after the rename' do
    verdicts = described_class.verdicts(described_class.facts_before(OpnsenseHostHandlerGuard::INSTALL_TASK_NAME))

    OpnsenseHostHandlerGuard::HANDLER_NAMES.each do |name|
      expect(verdicts[name]).to be(false),
                                "#{name} would restart the new binary under the old unit after a failed install"
    end
  end

  it 'clears the guard before the rename' do
    verdicts = described_class.verdicts(described_class.facts_before(OpnsenseHostHandlerGuard::RENAME_TASK_NAME))

    OpnsenseHostHandlerGuard::HANDLER_NAMES.each do |name|
      expect(verdicts[name]).to be(false), "#{name} is not held when the rename runs"
    end
  end

  it 'runs the restart handlers once install has written the units' do
    verdicts = described_class.verdicts(described_class.facts_before(nil))

    OpnsenseHostHandlerGuard::HANDLER_NAMES.each do |name|
      expect(verdicts[name]).to be(true), "#{name} stays held after install completed"
    end
  end
end
