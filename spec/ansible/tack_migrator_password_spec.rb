# frozen_string_literal: true

require_relative '../support/tack_search_inventory'

# `ops audit seed-roles` sets the password of the tack_migrator login from
# TACK_MIGRATOR_PASSWORD and refuses an empty value (TACK-554). Each
# environment renders its own vault key.
RSpec.describe 'the tack_migrator password in the Tack environment file' do
  {
    qa: 'render-only-vault_tack_qa_migrator_password',
    production: 'render-only-vault_tack_migrator_password'
  }.each do |environment, value|
    it "renders the #{environment} vault key on the owner guest" do
      rendered = TackSearchInventory.rendered(environment)

      expect(rendered.settings(rendered.owner)).to include('TACK_MIGRATOR_PASSWORD' => value)
    end
  end
end
