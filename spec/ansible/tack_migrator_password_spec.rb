# frozen_string_literal: true

require_relative '../support/tack_search_inventory'

RSpec.describe 'Tack credential references in the environment file' do
  %i[qa production].each do |environment|
    it "renders #{environment} file references without credential values", :aggregate_failures do
      rendered = TackSearchInventory.rendered(environment)
      settings = rendered.settings(rendered.owner)
      expect(settings.fetch('TACK_MIGRATOR_PASSWORD_FILE')).to eq('/run/secrets/TACK_MIGRATOR_PASSWORD')
      expect(settings.fetch('TACK_OPS_DATABASE_URL_FILE')).to eq('/run/secrets/TACK_OPS_DATABASE_URL')
      expect(settings.fetch('TACK_SECRETS_DIR')).to eq('/var/lib/tack/secrets')
      expect(settings.keys).not_to include('YUGABYTE_PASSWORD', 'TACK_MIGRATOR_PASSWORD', 'TACK_APP_PASSWORD',
                                           'AUDIT_OPERATOR_PASSWORD', 'AUDIT_WRITER_DSN', 'TACK_OPS_DATABASE_URL')
      expect(rendered.file(rendered.owner, 'env')).not_to include('render-only-vault_', TackSearchInventory::APP_PASSWORD)
    end
  end

  it 'provides the superuser password file only for explicit bootstrap or superuser operation', :aggregate_failures do
    rendered = TackSearchInventory.rendered(:qa, overrides: { 'tack_ledger_bootstrap' => true })
    expect(rendered.settings(rendered.owner).fetch('YUGABYTE_PASSWORD_FILE')).to eq('/run/secrets/YUGABYTE_PASSWORD')
    ordinary = TackSearchInventory.rendered(:qa)
    expect(ordinary.settings(ordinary.owner).keys).not_to include('YUGABYTE_PASSWORD_FILE')
  end
end
