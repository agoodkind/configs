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

  it 'connects the qa ops sidecar as tack_migrator', :aggregate_failures do
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

  it 'connects the production ops sidecar as the superuser until production has migration 019',
     :aggregate_failures do
    rendered = TackSearchInventory.rendered(:production)
    settings = rendered.settings(rendered.owner)

    expect(settings.fetch('TACK_OPS_DATABASE_URL')).to include('password=render-only-vault_tack_yugabyte_password')
    expect(settings).to include('YUGABYTE_PASSWORD' => 'render-only-vault_tack_yugabyte_password')
  end
end
