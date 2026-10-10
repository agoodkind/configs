# frozen_string_literal: true

require 'tmpdir'
require_relative '../support/routing_simulator_inventory'

RSpec.describe 'routing simulator validation task loader' do
  describe '.tasks' do
    def tasks_from(files)
      Dir.mktmpdir('routing-simulator-tasks') do |directory|
        files.each { |name, content| File.write(File.join(directory, name), content) }
        RoutingSimulatorInventory.tasks(directory: directory, entry_file: 'entry.yml')
      end
    end

    it 'returns the tasks of each imported file in entry order' do
      tasks = tasks_from(
        'entry.yml' => "- ansible.builtin.import_tasks: first.yml\n- ansible.builtin.import_tasks: second.yml\n",
        'first.yml' => "- name: one\n", 'second.yml' => "- name: two\n"
      )

      expect(tasks.map { |task| task.fetch('name') }).to eq(%w[one two])
    end

    it 'raises an error with the entry path when an entry lacks the import key' do
      expect { tasks_from('entry.yml' => "- name: no import\n") }
        .to raise_error(ArgumentError, %r{/entry\.yml: an entry lacks ansible\.builtin\.import_tasks})
    end

    it 'raises an error with the file path when an imported file is missing' do
      expect { tasks_from('entry.yml' => "- ansible.builtin.import_tasks: missing.yml\n") }
        .to raise_error(ArgumentError, %r{/missing\.yml: })
    end

    it 'raises an error with the file path when an imported file is invalid YAML' do
      files = { 'entry.yml' => "- ansible.builtin.import_tasks: broken.yml\n", 'broken.yml' => "- name: [\n" }

      expect { tasks_from(files) }.to raise_error(ArgumentError, %r{/broken\.yml: })
    end

    it 'raises an error with the file path when an imported file is not a list' do
      files = { 'entry.yml' => "- ansible.builtin.import_tasks: mapping.yml\n", 'mapping.yml' => "name: one\n" }

      expect { tasks_from(files) }.to raise_error(ArgumentError, %r{/mapping\.yml: the file is not a task list})
    end
  end
end
