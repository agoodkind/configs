# frozen_string_literal: true

require_relative '../support/tack_search_inventory'

# `ops audit seed-roles` refuses an empty TACK_MIGRATOR_PASSWORD (TACK-554).
RSpec.describe 'the tack_migrator password in the Tack environment file' do
  {
    qa: %w[render-only-vault_tack_qa_migrator_password render-only-vault_tack_qa_yugabyte_password],
    production: %w[render-only-vault_tack_migrator_password render-only-vault_tack_yugabyte_password]
  }.each do |environment, (migrator_value, _superuser_value)|
    it "renders the #{environment} vault key on the owner guest" do
      rendered = TackSearchInventory.rendered(environment)

      expect(rendered.settings(rendered.owner).fetch('TACK_MIGRATOR_PASSWORD')).to eq(migrator_value)
    end
  end

  it 'renders the QA ops sidecar URL with tack_migrator', :aggregate_failures do
    rendered = TackSearchInventory.rendered(:qa)
    url = rendered.settings(rendered.owner).fetch('TACK_OPS_DATABASE_URL')

    expect(url).to include('user=tack_migrator', 'password=render-only-vault_tack_qa_migrator_password')
  end

  it 'does not render superuser credentials for qa', :aggregate_failures do
    rendered = TackSearchInventory.rendered(:qa)
    hosts = [rendered.owner] + rendered.members.map { |member| rendered.member_host(member) }

    hosts.each do |host|
      expect(rendered.settings(host).keys).not_to include('YUGABYTE_PASSWORD', 'YUGABYTE_USER')
      expect(rendered.file(host, 'env')).not_to include('render-only-vault_tack_qa_yugabyte_password')
    end
  end

  it 'renders the production ops sidecar URL with the superuser', :aggregate_failures do
    rendered = TackSearchInventory.rendered(:production)
    settings = rendered.settings(rendered.owner)

    expect(settings.fetch('TACK_OPS_DATABASE_URL'))
      .to include('user=yugabyte', 'password=render-only-vault_tack_yugabyte_password')
    expect(settings).to include('YUGABYTE_USER' => 'yugabyte',
                                'YUGABYTE_PASSWORD' => 'render-only-vault_tack_yugabyte_password')
  end

  # Provision on a from-empty rebuild reads TACK_OPS_DATABASE_URL from the
  # .env before any run has created tack_migrator.
  it 'renders the QA ops sidecar URL with the superuser on a ledger bootstrap run', :aggregate_failures do
    rendered = TackSearchInventory.rendered(:qa, overrides: { 'tack_ledger_bootstrap' => true })
    url = rendered.settings(rendered.owner).fetch('TACK_OPS_DATABASE_URL')

    expect(url).to include('user=yugabyte', 'password=render-only-vault_tack_qa_yugabyte_password')
  end
end
