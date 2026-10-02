# frozen_string_literal: true

require_relative '../support/tack_ops_identity'

# These examples evaluate the conditions of the shared identity guard with
# the agent shell marker set or removed.
RSpec.describe TackOpsIdentityFlags do
  let(:agent) { { service: TackOpsIdentityFlags::AGENT_SERVICE, session: 's1', agent_run: true } }

  it 'refuses an agent shell without an agent service and accepts an agent shell with one', :aggregate_failures do
    expect(described_class.guard_passes?(agent_run: true)).to be(false)
    expect(described_class.guard_passes?(**agent)).to be(true)
    expect(described_class.guard_passes?).to be(true)
  end

  it 'refuses a service without a session, a session without a service, and an invalid service name', :aggregate_failures do
    expect(described_class.guard_passes?(service: 'claude-rowan', agent_run: true)).to be(false)
    expect(described_class.guard_passes?(session: 's1')).to be(false)
    expect(described_class.guard_passes?(service: 'Claude Rowan', session: 's1', agent_run: true)).to be(false)
  end

  it 'refuses a session that is not one token of letters, digits, and hyphens', :aggregate_failures do
    expect(described_class.guard_passes?(service: 'claude-rowan', session: TackOpsIdentityFlags::SESSION)).to be(true)
    ['a b', 'a;reboot', "a\nreboot", 'a$(id)', 'a' * 65].each do |session|
      expect(described_class.guard_passes?(service: 'claude-rowan', session: session)).to be(false)
    end
  end

  it 'refuses an accountable email on a human deploy and requires one on an agent run', :aggregate_failures do
    expect(described_class.guard_passes?(replace: { 'accountable' => TackOpsIdentityFlags::ACCOUNTABLE_EMAIL })).to be(false)
    expect(described_class.guard_passes?(**agent, replace: { 'accountable' => '' })).to be(false)
  end

  it 'refuses an operator id that does not derive from the operator email' do
    flags = described_class.rendered_flags(service: TackOpsIdentityFlags::AGENT_SERVICE, session: 's1')
    other = flags.sub(described_class.operator_id(TackOpsIdentityFlags::ACCOUNTABLE_EMAIL), described_class.operator_id('x@y.z'))
    expect(described_class.guard_passes?(**agent, replace: { 'flags' => other })).to be(false)
  end

  it 'refuses an empty or malformed operator email', :aggregate_failures do
    ['', 'no-at-sign', 'two@at@signs', "human@example.invalid\n"].each do |email|
      expect(described_class.guard_passes?(replace: { 'email' => email })).to be(false)
    end
  end
end
