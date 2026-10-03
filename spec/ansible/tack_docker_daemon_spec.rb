# frozen_string_literal: true

require 'json'
require 'yaml'
require_relative '../support/ansible_render'

# The Docker daemon settings every Tack guest receives from deploy-tack.yml.
module TackDockerDaemon
  PLAYBOOK_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'deploy-tack.yml')
  DAEMON_FILE = '/etc/docker/daemon.json'
  COPY_MODULE = 'ansible.builtin.copy'
  RESTART_HANDLER = 'Restart Docker'

  module_function

  def tasks_in(tasks)
    tasks.flat_map { |task| [task] + tasks_in(task['block'] || []) + tasks_in(task['always'] || []) }
  end

  def daemon_task
    plays = YAML.safe_load_file(PLAYBOOK_FILE)
    tasks = plays.flat_map { |play| tasks_in(play['tasks'] || []) }
    task = tasks.find { |candidate| candidate.dig(COPY_MODULE, 'dest') == DAEMON_FILE }
    raise "#{PLAYBOOK_FILE} has no task that writes #{DAEMON_FILE}" if task.nil?

    task
  end

  def daemon_settings
    JSON.parse(daemon_task.fetch(COPY_MODULE).fetch('content'))
  end
end

RSpec.describe TackDockerDaemon do
  it 'writes the IPv6 settings and the containerd image store to daemon.json', :aggregate_failures do
    settings = described_class.daemon_settings

    expect(settings.fetch('ipv6')).to be(true)
    expect(settings.fetch('ip6tables')).to be(true)
    expect(settings.dig('features', 'containerd-snapshotter')).to be(true)
  end

  it 'restarts Docker when daemon.json changes' do
    expect(described_class.daemon_task.fetch('notify')).to eq(TackDockerDaemon::RESTART_HANDLER)
  end
end
