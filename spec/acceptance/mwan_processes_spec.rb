# frozen_string_literal: true

require 'tmpdir'
require 'open3'
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

  context 'with an exited group leader' do
    def wait_process_state(pid, exited:)
      deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 5
      loop do
        output, error, status = Open3.capture3('ps', '-p', pid.to_s, '-o', 'stat=')
        return if exited && status.success? && output.strip.start_with?('Z')
        return if !exited && status.exitstatus == 1 && output.empty? && error.empty?

        raise "Unexpected process probe failure: #{error}" unless status.success?
        raise "Process #{pid} did not enter the expected state" if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

        sleep 0.01
      end
    end

    def descendant_program
      <<~RUBY
        $stdout.sync = true
        fork do
          Signal.trap('TERM', 'IGNORE')
          puts Process.pid
          loop { sleep 1 }
        end
        exit 0
      RUBY
    end

    it 'terminates a TERM-resistant descendant after the group leader exits' do
      Dir.mktmpdir do |directory|
        processes = MwanAcceptance::Processes.new(directory)
        pid, stem = processes.start([RbConfig.ruby, '-e', descendant_program], 'fork-leader')
        wait_process_state(pid, exited: true)
        descendant = Integer(File.read("#{stem}.stdout"))
        expect(Process.getpgid(descendant)).to eq(pid)
        processes.cleanup
        expect(JSON.parse(File.read("#{stem}.status.json"))).to include('exit_status' => 0, 'cleanup' => true)
        wait_process_state(descendant, exited: false)
        expect { Process.waitpid(pid, Process::WNOHANG) }.to raise_error(Errno::ECHILD)
      ensure
        if descendant
          begin
            Process.kill('KILL', descendant)
          rescue Errno::ESRCH
            nil
          end
        end
      end
    end
  end
end
