# frozen_string_literal: true

require 'timeout'
require 'tmpdir'
require_relative '../support/ansible_render'

RSpec.describe 'routing simulator home route tracker' do
  script = File.join(AnsibleRender::REPOSITORY_ROOT, 'testbed', 'routing-simulators', 'home-route-tracker.sh')
  fixtures = File.join(AnsibleRender::REPOSITORY_ROOT, 'spec', 'fixtures', 'routing_simulator')
  arguments = %w[2001:db8:100::/48 2001:db8:1ff:1::1 sit-test 30 3 100].freeze
  wait_seconds = 10

  def wait_until(seconds)
    Timeout.timeout(seconds) { sleep 0.05 until yield }
  end

  define_method(:stop_tracker) do |signal|
    Dir.mktmpdir('home-route-tracker') do |directory|
      log = File.join(directory, 'ip.log')
      environment = { 'PATH' => "#{fixtures}:#{ENV.fetch('PATH')}", 'FAKE_IP_LOG' => log,
                      'FAKE_IP_ROUTE' => File.join(directory, 'route') }
      pid = Process.spawn(environment, script, *arguments, out: File::NULL, err: File::NULL)
      wait_until(wait_seconds) { File.exist?(log) && File.read(log).include?('route replace') }
      Process.kill(signal, pid)
      _, status = Timeout.timeout(wait_seconds) { Process.wait2(pid) }
      { status: status, commands: File.readlines(log, chomp: true) }
    end
  end

  it 'removes the route and exits with status 0 after TERM', :aggregate_failures do
    result = stop_tracker('TERM')

    expect(result.fetch(:commands).last).to eq(
      '-6 route del 2001:db8:100::/48 via 2001:db8:1ff:1::1 dev sit-test metric 100 proto static'
    )
    expect(result.fetch(:status).exitstatus).to eq(0)
  end

  it 'removes the route and exits with status 130 after INT', :aggregate_failures do
    result = stop_tracker('INT')

    expect(result.fetch(:commands).last).to start_with('-6 route del 2001:db8:100::/48')
    expect(result.fetch(:status).exitstatus).to eq(130)
  end
end
