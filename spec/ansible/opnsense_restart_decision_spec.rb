# frozen_string_literal: true

require 'json'
require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# These checks evaluate the OPNsense deploy's restart decision with ansible-core's
# own templar. They read the set_fact tasks and the condition lists from the real
# task file, so a change to any expression in that chain changes what is tested.
# Only the registered results of the tasks that talk to the router and the
# hypervisor are supplied, shaped the way those tasks register them.
module OpnsenseRestartDecision
  TASK_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'tasks', 'mwan-opnsense-deploy.yml')
  RESTART_TASK_NAME = 'Restart the daemon onto the incoming binary'
  MARK_HEALTHY_TASK_NAME = 'Mark the running daemon binary healthy'
  RESTART_CONDITION = 'restart_when'
  MARK_HEALTHY_CONDITION = 'mark_healthy_changed_when'
  RELEASE_COMMIT = '9b12ead3d6525538d098bb80601b373efae6d6d1'
  # The line the testbed router daemon printed at 2026-09-14 23:51 PT, after an
  # earlier install replaced its binary without a restart.
  STALE_VERSION_LINE = 'version=commit=e15d495 dirty=clean binhash=unknown ' \
                       'commit=e15d495 dirty=false binhash=unknown'
  RELEASE_VERSION_LINE = 'version=commit=9b12ead dirty=clean binhash=059926869ac5 ' \
                         'commit=9b12ead dirty=false binhash=059926869ac5'
  VERSION_READ_ERROR = 'rpc error: code = DeadlineExceeded desc = context deadline exceeded'
  RELEASE_CHECKSUM = '2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881'
  OLDER_CHECKSUM = '18ac3e7343f016890c510e93f935261169d9e3f565436429830faf0934f4f8e4'

  # The part of the task file that decides whether the daemon restarts: every
  # set_fact task before the restart block, in file order, the restart block's
  # when list, and the mark-healthy task's changed_when list.
  Decision = Struct.new(:facts, :restart_when, :mark_healthy_when, :found_restart, :found_mark_healthy, keyword_init: true)

  module_function

  CASES = [
    {
      name: 'incoming binary differs from .current', current_checksum: OLDER_CHECKSUM, instances_exit: 0,
      version: TaskExpressions.command_result(0, RELEASE_VERSION_LINE, ''), want_restart: true, want_mark_changed: true
    },
    {
      name: 'guest has no .current', current_checksum: nil, instances_exit: 0,
      version: TaskExpressions.command_result(0, RELEASE_VERSION_LINE, ''), want_restart: true, want_mark_changed: true
    },
    {
      name: 'instance check failed', current_checksum: RELEASE_CHECKSUM, instances_exit: 1,
      version: TaskExpressions.command_result(0, RELEASE_VERSION_LINE, ''), want_restart: true, want_mark_changed: true
    },
    {
      name: 'running daemon reports a stale commit', current_checksum: RELEASE_CHECKSUM, instances_exit: 0,
      version: TaskExpressions.command_result(0, STALE_VERSION_LINE, ''), want_restart: true, want_mark_changed: true
    },
    {
      name: 'version read failed', current_checksum: RELEASE_CHECKSUM, instances_exit: 0,
      version: TaskExpressions.command_result(1, '', VERSION_READ_ERROR), want_restart: true, want_mark_changed: true
    },
    {
      name: 'daemon already runs the release', current_checksum: RELEASE_CHECKSUM, instances_exit: 0,
      version: TaskExpressions.command_result(0, RELEASE_VERSION_LINE, ''), want_restart: false, want_mark_changed: false
    }
  ].freeze

  # The looped stat result for .current then .incoming, shaped the way the stat
  # task registers it. A missing .current has no checksum key.
  def binaries_result(current_checksum)
    current_stat = { 'exists' => false }
    current_stat = { 'exists' => true, 'checksum' => current_checksum } unless current_checksum.nil?
    incoming_stat = { 'exists' => true, 'checksum' => RELEASE_CHECKSUM }
    { 'results' => [{ 'stat' => current_stat }, { 'stat' => incoming_stat }], 'changed' => false }
  end

  def read_decision
    decision = Decision.new(facts: [], restart_when: [], mark_healthy_when: [], found_restart: false, found_mark_healthy: false)
    collect(decision, YAML.safe_load_file(TASK_FILE))
    raise "#{TASK_FILE} has no task #{RESTART_TASK_NAME.inspect} with a when list" unless decision.found_restart
    raise "#{TASK_FILE} has no task #{RESTART_TASK_NAME.inspect} with a when list" if decision.restart_when.empty?
    raise "#{TASK_FILE} has no task #{MARK_HEALTHY_TASK_NAME.inspect} with a changed_when" unless decision.found_mark_healthy
    raise "#{TASK_FILE} has no task #{MARK_HEALTHY_TASK_NAME.inspect} with a changed_when" if decision.mark_healthy_when.empty?

    decision
  end

  def collect(decision, tasks)
    tasks.each do |task|
      if task['name'] == RESTART_TASK_NAME
        decision.restart_when = TaskExpressions.condition_list(task['when'])
        decision.found_restart = true
      elsif task['name'] == MARK_HEALTHY_TASK_NAME
        decision.mark_healthy_when = TaskExpressions.condition_list(task['changed_when'])
        decision.found_mark_healthy = true
      elsif !task[TaskExpressions::SET_FACT_KEY].nil? && !decision.found_restart
        decision.facts << TaskExpressions.fact_task(task)
      end
      collect(decision, task['block'] || [])
    end
  end

  def evaluate(decision, variables)
    TaskExpressions.evaluate(
      variables: variables,
      facts: decision.facts,
      conditions: { RESTART_CONDITION => decision.restart_when, MARK_HEALTHY_CONDITION => decision.mark_healthy_when }
    )
  end
end

RSpec.describe OpnsenseRestartDecision do
  before(:all) do
    @decision = described_class.read_decision
  end

  OpnsenseRestartDecision::CASES.each do |test_case|
    it "decides the restart when #{test_case[:name]}", :aggregate_failures do
      variables = {
        'ansible_check_mode' => false,
        'opnsensectl_release_commit' => OpnsenseRestartDecision::RELEASE_COMMIT,
        'mwan_opnsense_binaries' => described_class.binaries_result(test_case[:current_checksum]),
        'mwan_opnsense_instances_before' => TaskExpressions.command_result(test_case[:instances_exit], '', ''),
        'mwan_opnsense_version_before' => test_case[:version]
      }
      result = described_class.evaluate(@decision, variables)
      verdicts = result['conditions'] || {}
      facts_text = JSON.generate(result['facts'])

      raise "evaluator returned no #{OpnsenseRestartDecision::RESTART_CONDITION} verdict: #{result.inspect}" unless verdicts.key?(OpnsenseRestartDecision::RESTART_CONDITION)

      restart = verdicts[OpnsenseRestartDecision::RESTART_CONDITION]
      expect(restart).to be(test_case[:want_restart]),
                         "restart = #{restart}, want #{test_case[:want_restart]} (when #{@decision.restart_when.inspect}, facts #{facts_text})"

      raise "evaluator returned no #{OpnsenseRestartDecision::MARK_HEALTHY_CONDITION} verdict: #{result.inspect}" unless verdicts.key?(OpnsenseRestartDecision::MARK_HEALTHY_CONDITION)

      mark_changed = verdicts[OpnsenseRestartDecision::MARK_HEALTHY_CONDITION]
      expect(mark_changed).to be(test_case[:want_mark_changed]),
                              "mark-healthy changed = #{mark_changed}, want #{test_case[:want_mark_changed]} " \
                              "(changed_when #{@decision.mark_healthy_when.inspect}, facts #{facts_text})"
    end
  end
end
