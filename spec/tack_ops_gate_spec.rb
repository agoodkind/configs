# frozen_string_literal: true

require 'tmpdir'
require_relative 'support/command_runner'

RSpec.describe 'tack/tack-ops-gate.sh' do
  gate = File.expand_path('../tack/tack-ops-gate.sh', __dir__)
  refused_status = 126

  def run_gate(gate, command, install_dir)
    environment = command.nil? ? {} : { 'SSH_ORIGINAL_COMMAND' => command }
    CommandRunner.run(environment, ['bash', gate, install_dir], chdir: install_dir, timeout_seconds: 60)
  end

  [
    'bash',
    'sh -c id',
    'docker exec tack-yugabyte-1 ysqlsh',
    'ysqlsh -c select',
    'psql',
    './server migrate',
    './server ops',
    './server ops search verify; id',
    './server ops search verify && bash',
    './server ops db sql --statement "drop table users"',
    'docker compose run --rm yugabyte ops x',
    'docker compose run --rm tack-ops migrate',
    'docker compose exec app ops search verify',
    './server ops search verify $(id)'
  ].each do |command|
    it "refuses #{command.inspect}", :aggregate_failures do
      Dir.mktmpdir('tack-ops-gate') do |install_dir|
        result = run_gate(gate, command, install_dir)

        expect(result.exit_status.exitstatus).to eq(refused_status)
        expect(result.output).to include('tack-ops-gate: refused')
      end
    end
  end

  [nil, '   '].each do |command|
    it "rejects a session that does not specify a command (#{command.inspect})", :aggregate_failures do
      Dir.mktmpdir('tack-ops-gate') do |install_dir|
        result = run_gate(gate, command, install_dir)

        expect(result.exit_status.exitstatus).to eq(refused_status)
        expect(result.output).to include('an interactive shell')
      end
    end
  end

  [
    './server ops search verify',
    'docker compose run --rm tack-ops ops deploy verify',
    'docker compose run --rm app ops qa datagen seed --scale small'
  ].each do |command|
    it "passes #{command.inspect} to docker compose", :aggregate_failures do
      Dir.mktmpdir('tack-ops-gate') do |install_dir|
        result = run_gate(gate, command, install_dir)

        expect(result.exit_status.exitstatus).not_to eq(refused_status)
        expect(result.output).not_to include('tack-ops-gate: refused')
        expect(result.output).to include('no configuration file provided')
      end
    end
  end
end
