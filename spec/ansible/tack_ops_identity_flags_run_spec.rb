# frozen_string_literal: true

require_relative '../support/tack_ops_identity'

# These examples run the real deploy-tack playbook in check mode, with the
# agent shell marker set, against an inventory that selects no guest.
RSpec.describe TackOpsIdentityFlags do
  it 'stops a deploy from an agent shell without a service before any other task runs', :aggregate_failures do
    result = described_class.run_deploy(described_class.identity(service: '', session: ''), agent: true)

    expect(result.exit_status.success?).to be(false)
    expect(result.output).to include("TASK [#{TackOpsIdentityFlags::GUARD}]", TackOpsIdentityFlags::GUARD_MESSAGE)
    expect(result.output).not_to include('TASK [Group each host')
  end

  it 'lets a deploy from an agent shell with a service and session continue past the check', :aggregate_failures do
    identity = described_class.identity(service: TackOpsIdentityFlags::AGENT_SERVICE, session: TackOpsIdentityFlags::SESSION)
    result = described_class.run_deploy(identity, agent: true)

    expect(result.exit_status.success?).to be(true), result.output
    expect(result.output).to include('TASK [Group each host')
  end

  it 'stops an agent shell deploy that passes tack_ops_agent_run=false and no service', :aggregate_failures do
    identity = described_class.identity(service: '', session: '', overrides: { 'tack_ops_agent_run' => false })
    result = described_class.run_deploy(identity, agent: true)

    expect(result.exit_status.success?).to be(false)
    expect(result.output).to include(TackOpsIdentityFlags::GUARD_MESSAGE)
    expect(result.output).not_to include('TASK [Group each host')
  end

  it 'stops an agent shell deploy that replaces tack_ops_identity_flags', :aggregate_failures do
    described_class.replaced_flags.each do |label, flags|
      identity = described_class.identity(service: TackOpsIdentityFlags::AGENT_SERVICE, session: TackOpsIdentityFlags::SESSION,
                                          overrides: { 'tack_ops_identity_flags' => flags })
      result = described_class.run_deploy(identity, agent: true)

      expect(result.exit_status.success?).to be(false), label
      expect(result.output).to include(TackOpsIdentityFlags::GUARD_MESSAGE), label
    end
  end
end
