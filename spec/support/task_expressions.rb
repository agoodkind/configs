# frozen_string_literal: true

require 'json'
require_relative 'ansible_render'
require_relative 'command_runner'

# Reads the expressions a real task file carries and evaluates them with
# ansible-core's own templar through the evaluator fixture, so a spec tests the
# exact text a deploy runs.
module TaskExpressions
  EVALUATOR = File.join(AnsibleRender::FIXTURE_DIRECTORY, 'evaluate_task_conditions.py')
  SET_FACT_KEY = 'ansible.builtin.set_fact'

  module_function

  # A registered ansible.builtin.command or script result.
  def command_result(exit_code, stdout, stderr)
    { 'changed' => false, 'failed' => false, 'rc' => exit_code, 'stdout' => stdout, 'stderr' => stderr }
  end

  # One set_fact task the way the evaluator renders it: its when list, its task
  # vars, and the facts it sets.
  def fact_task(task)
    { 'when' => condition_list(task['when']), 'vars' => string_map(task['vars']), 'set_fact' => string_map(task[SET_FACT_KEY]) }
  end

  # One set of templates the evaluator renders after the facts, with the task's
  # own vars in scope.
  def render_task(task, templates)
    { 'vars' => string_map(task['vars']), 'templates' => string_map(templates) }
  end

  # A when, changed_when, or similar field, which Ansible accepts as one
  # expression or a list of them.
  def condition_list(value)
    return [] if value.nil?
    return value.map { |item| scalar_text(item) } if value.is_a?(Array)

    [scalar_text(value)]
  end

  def string_map(value)
    return {} if value.nil?

    value.transform_values { |item| scalar_text(item) }
  end

  def scalar_text(value)
    return '' if value.nil?

    value.to_s
  end

  def evaluate(variables:, facts:, conditions: {}, renders: [])
    payload = JSON.generate('variables' => variables, 'facts' => facts, 'conditions' => conditions, 'renders' => renders)
    result = CommandRunner.capture(
      [AnsibleRender.ansible_python, EVALUATOR],
      stdin_data: payload,
      chdir: AnsibleRender::REPOSITORY_ROOT,
      timeout_seconds: AnsibleRender::PLAYBOOK_TIMEOUT_SECONDS
    )
    raise "evaluate task expressions exceeded #{AnsibleRender::PLAYBOOK_TIMEOUT_SECONDS}s\n#{result.error_output}" if result.timed_out
    raise "evaluate task expressions: #{result.exit_status}\n#{result.error_output}" unless result.exit_status.success?

    decode(result.output)
  end

  def decode(output)
    JSON.parse(output)
  rescue JSON::ParserError => e
    raise "decode evaluator output: #{e.message}\n#{output}"
  end
end
