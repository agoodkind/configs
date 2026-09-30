# frozen_string_literal: true

require 'tmpdir'
require_relative '../../lib/mwan_acceptance'

RSpec.describe 'MWAN acceptance process cleanup' do
  def wait_child_ready(stem)
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 5
    until File.read("#{stem}.stdout").include?('ready')
      raise 'The cleanup child did not start' if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

      sleep 0.01
    end
  end

  def cleanup_child(arguments)
    Dir.mktmpdir do |directory|
      processes = MwanAcceptance::Processes.new(directory)
      pid, stem = processes.start(arguments, 'cleanup-child')
      wait_child_ready(stem)
      started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
      processes.cleanup
      elapsed = Process.clock_gettime(Process::CLOCK_MONOTONIC) - started
      status = JSON.parse(File.read("#{stem}.status.json"))
      expect(status.fetch('pid')).to eq(pid)
      expect { Process.waitpid(pid, Process::WNOHANG) }.to raise_error(Errno::ECHILD)
      pid = nil
      [status, elapsed]
    ensure
      if pid && !Process.waitpid(pid, Process::WNOHANG)
        Process.kill('KILL', -pid)
        Process.waitpid(pid)
      end
    end
  end

  it 'reaps a real child that exits after TERM before signaling KILL' do
    status, elapsed = cleanup_child([RbConfig.ruby, '-e', '$stdout.sync = true; puts "ready"; sleep 30'])
    expect(status).to include('signal' => Signal.list.fetch('TERM'), 'cleanup' => true)
    expect(elapsed).to be < 2
  end

  it 'kills and reaps a real child that ignores TERM within the cleanup deadline' do
    status, elapsed = cleanup_child([RbConfig.ruby, '-e', 'Signal.trap("TERM", "IGNORE"); $stdout.sync = true; puts "ready"; sleep 30'])
    expect(status).to include('signal' => Signal.list.fetch('KILL'), 'cleanup' => true)
    expect(elapsed).to be < 2
  end
end
