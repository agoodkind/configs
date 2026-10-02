# frozen_string_literal: true

require_relative '../support/tack_ops_identity'

# These examples render the operator identity flags of tack_all.yml for a
# human deploy and an agent deploy.
RSpec.describe TackOpsIdentityFlags do
  let(:accountable_id) { described_class.operator_id(TackOpsIdentityFlags::ACCOUNTABLE_EMAIL) }
  let(:human_id) { described_class.operator_id(TackOpsIdentityFlags::HUMAN_EMAIL) }

  it 'records the human operator when no agent service is set' do
    flags = described_class.flags(service: '', session: '',
                                  overrides: { 'deploy_operator_email' => TackOpsIdentityFlags::HUMAN_EMAIL })

    expect(flags).to eq(
      ['--operator-id', human_id, '--operator-email', TackOpsIdentityFlags::HUMAN_EMAIL,
       '--operator-name', '"Human', 'Operator"', '--deploy-commit', TackOpsIdentityFlags::COMMIT]
    )
  end

  it 'records the agent service and session as the actor and the accountable person as on_behalf_of' do
    expect(described_class.flags(service: 'claude-rowan', session: TackOpsIdentityFlags::SESSION)).to eq(
      ['--operator-service', 'claude-rowan', '--operator-session', TackOpsIdentityFlags::SESSION,
       '--operator-id', accountable_id, '--operator-email', TackOpsIdentityFlags::ACCOUNTABLE_EMAIL,
       '--deploy-commit', TackOpsIdentityFlags::COMMIT]
    )
  end

  it 'renders the operator ID the Tack command line derives for the same email' do
    flags = described_class.flags(service: '', session: '',
                                  overrides: { 'deploy_operator_email' => 'goodkindalex@gmail.com' })

    expect(flags.each_cons(2)).to include(['--operator-id', 'b8cfe465-3681-5eb2-89da-0b228a7c8d0f'])
  end
end
