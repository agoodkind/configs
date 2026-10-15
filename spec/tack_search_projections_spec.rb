# frozen_string_literal: true

require 'digest'
require 'json'

# The production manifest declares the search decision of each seeded
# property definition of the production org. Tack derives each seeded ID as a
# version 5 UUID of "<org>:<name>" in the system property namespace.
module TackSearchProjections
  MANIFEST_FILE = File.expand_path('../tack/search-projections/production.json', __dir__)
  PRODUCTION_ORG = '019dc5ad-0408-7e43-9c4d-d3e6736ac058'
  SYSTEM_PROPERTY_NAMESPACE = '7ac0face-dead-beef-cafe-000000000000'
  SEEDED_NAMES = %w[color description due_date group identifier is_draft parent_id priority
                    scope_id sequence slug sort_order start_date state_id].freeze

  module_function

  def uuid_v5(namespace, name)
    digest = Digest::SHA1.digest([namespace.delete('-')].pack('H*') + name).bytes.first(16)
    digest[6] = (digest[6] & 0x0f) | 0x50
    digest[8] = (digest[8] & 0x3f) | 0x80
    hex = digest.pack('C*').unpack1('H*')
    [hex[0, 8], hex[8, 4], hex[12, 4], hex[16, 4], hex[20, 12]].join('-')
  end

  def seeded_ids
    SEEDED_NAMES.to_h { |name| [uuid_v5(SYSTEM_PROPERTY_NAMESPACE, "#{PRODUCTION_ORG}:#{name}"), name] }
  end
end

RSpec.describe TackSearchProjections do
  it 'lists one sorted entry for each seeded property definition of the production org', :aggregate_failures do
    entries = JSON.parse(File.read(TackSearchProjections::MANIFEST_FILE))
    identities = entries.map { |entry| [entry.fetch('org_id'), entry.fetch('property_def_id')] }

    expect(entries).to be_an(Array)
    expect(identities).to eq(identities.sort)
    expect(identities.uniq.size).to eq(TackSearchProjections::SEEDED_NAMES.size)
    expect(identities.map(&:first).uniq).to eq([TackSearchProjections::PRODUCTION_ORG])
    expect(identities.map(&:last)).to match_array(described_class.seeded_ids.keys)
  end
end
