# frozen_string_literal: true

require_relative '../support/routing_simulator_config'

RSpec.describe 'routing simulator gateway restart check' do
  let(:inventory) { RoutingSimulatorConfig.inventory }

  def report_task(name)
    RoutingSimulatorConfig.task(name, 'tasks', 'check-routing-recovery-ifmgr-restart-report.yml')
  end

  def captures(outbound, inbound)
    results = { 'outbound' => outbound, 'inbound' => inbound }.map do |direction, times|
      lines = times.map { |time| "#{time} IP6 2001:db8::1 > 2001:db8::2: ICMP6, echo request, id 7, seq 1, length 64" }
      { 'item' => { 'item' => { 'direction' => direction } }, 'stdout' => (['listening on any'] + lines).join("\n") }
    end
    { 'results' => results }
  end

  def measure(samples, outbound: %w[100.0 100.2 100.4], inbound: %w[100.0 100.2 100.4])
    variables = {
      'routing_ifmgr_restart_captures' => captures(outbound, inbound),
      'routing_ifmgr_restart_samples' => TaskExpressions.command_result(0, samples.join("\n"), ''),
      'routing_ifmgr_restart_before' => TaskExpressions.command_result(0, '102.000000000', '')
    }
    facts = [TaskExpressions.fact_task(report_task('Measure the echo request gaps and the route absences'))]
    TaskExpressions.evaluate(variables: variables, facts: facts).fetch('facts').fetch('routing_ifmgr_restart_report')
  end

  def kept?(report, upstream_nodes)
    assertion = report_task('Require the home prefix upstream and the gateway BGP default routes during the restart')
    variables = { 'routing_ifmgr_restart_report' => report, 'routing_recovery' => { 'upstream_nodes' => upstream_nodes },
                  'routing_ifmgr_restart_samples' => { 'stdout' => '' } }
    RoutingSimulatorConfig.assertion_valid?(inventory, assertion, variables, [])
  end

  it 'reports the largest request gap in each direction', :aggregate_failures do
    report = measure(['101.0 gateway 2', '101.0 upstream-913 present'],
                     outbound: %w[100.0 100.2 103.7 103.9], inbound: %w[100.0 100.2 100.4])

    expect(report.fetch('gaps')).to eq('outbound' => 3.5, 'inbound' => 0.2)
  end

  it 'accepts a restart that keeps every route', :aggregate_failures do
    samples = ['101.0 upstream-913 present', '101.0 gateway 2', '103.0 upstream-913 present', '103.0 gateway 2',
               '105.0 upstream-913 present', '105.0 gateway 3']
    report = measure(samples)

    expect(report.fetch('gateway_baseline')).to eq(2)
    expect(report.fetch('absences')).to eq('upstream-913' => { 'missing' => false, 'seconds' => 0.0 },
                                           'gateway' => { 'missing' => false, 'seconds' => 0.0 })
    expect(kept?(report, ['tunnel_vps_upstream'])).to be(true)
  end

  it 'rejects a restart that removes the home prefix upstream or a gateway default route', :aggregate_failures do
    upstream_loss = measure(['101.0 upstream-913 present', '101.0 gateway 2', '103.0 upstream-913 absent',
                             '103.0 gateway 2', '104.5 upstream-913 present', '104.5 gateway 2'])
    gateway_loss = measure(['101.0 upstream-913 present', '101.0 gateway 2', '103.0 upstream-913 present',
                            '103.0 gateway 1', '106.0 upstream-913 present', '106.0 gateway 2'])

    expect(upstream_loss.fetch('absences').fetch('upstream-913')).to eq('missing' => true, 'seconds' => 1.5)
    expect(gateway_loss.fetch('absences').fetch('gateway')).to eq('missing' => true, 'seconds' => 3.0)
    expect(kept?(upstream_loss, ['tunnel_vps_upstream'])).to be(false)
    expect(kept?(gateway_loss, ['tunnel_vps_upstream'])).to be(false)
  end

  it 'rejects a measurement without a sample from each upstream router' do
    report = measure(['101.0 gateway 2', '103.0 gateway 2'])

    expect(kept?(report, ['tunnel_vps_upstream'])).to be(false)
  end
end
