# frozen_string_literal: true

require 'tmpdir'
require 'yaml'
require_relative '../support/ansible_render'

# The app reaches the ledger over a multi-host connection string. Stopping one
# data guest black-holes its socket rather than refusing it, so with no bound
# on each name the driver waits out the kernel's TCP retry on the first name
# and never reaches a surviving node. This check renders the real template
# against the real group_vars and asserts the deploy writes that bound.
module TackOverride
  GROUP_VARS_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'inventory', 'group_vars', 'tack_all.yml')
  TEMPLATE_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'tack', 'docker-compose.override.yml.j2')
  CONNECT_TIMEOUT_KEY = 'tack_database_connect_timeout_seconds'
  LEDGER_NAMES = 'yb1:5433,yb2:5433,yb3:5433'
  # A repointed owner guest: the app dials the three data nodes.
  OWNER_GUEST_VARS = {
    'tack_provision_owner' => true,
    'tack_ledger_consumers_repointed' => true,
    'tack_ledger_legacy_node_present' => false,
    'tack_ledger_node_name' => 'yugabyte',
    'tack_ledger_join_target' => '',
    'tack_store_host' => '3d06:bad:b01:10::20',
    'tack_ledger_node_addresses' => {
      'yb1' => '3d06:bad:b01:10::21',
      'yb2' => '3d06:bad:b01:10::22',
      'yb3' => '3d06:bad:b01:10::23'
    }
  }.freeze

  module_function

  # Reads the bound from the group_vars file that owns it, so the assertion is
  # tied to that one durable home.
  def connect_timeout_seconds
    group_vars = YAML.safe_load_file(GROUP_VARS_FILE, aliases: true)
    seconds = group_vars[CONNECT_TIMEOUT_KEY]
    raise "#{GROUP_VARS_FILE} does not set #{CONNECT_TIMEOUT_KEY}" if seconds.nil?
    raise "#{CONNECT_TIMEOUT_KEY} = #{seconds.inspect}, want a whole number of seconds" unless seconds.is_a?(Integer)
    raise "#{CONNECT_TIMEOUT_KEY} = #{seconds}, want a positive whole number of seconds" unless seconds.positive?

    seconds
  end

  # Renders the compose override for a repointed owner guest, the shape that
  # carries the app's multi-host connection string.
  def rendered_database_url
    Dir.mktmpdir('tack-override') do |output_directory|
      output_file = File.join(output_directory, 'docker-compose.override.yml')
      AnsibleRender.render(
        inventory: 'localhost,',
        playbook: 'render_tack_override.yml',
        extra_vars: OWNER_GUEST_VARS.merge(
          'group_vars_file' => GROUP_VARS_FILE,
          'template_file' => TEMPLATE_FILE,
          'output_file' => output_file
        )
      )
      rendered = YAML.safe_load_file(output_file)
      rendered.dig('services', 'app', 'environment', 'DATABASE_URL').to_s
    end
  end
end

RSpec.describe TackOverride do
  it 'renders the ledger connect bound into the app database URL' do
    connect_timeout_seconds = described_class.connect_timeout_seconds
    database_url = described_class.rendered_database_url
    want_bound = "connect_timeout=#{connect_timeout_seconds}"

    expect(database_url).not_to be_empty, 'rendered override carries no app DATABASE_URL'
    aggregate_failures do
      expect(database_url).to include(TackOverride::LEDGER_NAMES),
                              "app DATABASE_URL = #{database_url.inspect}, want the three ledger names"
      expect(database_url).to include(want_bound),
                              "app DATABASE_URL = #{database_url.inspect}, want the per-name connect bound " \
                              "#{want_bound.inspect}; without it the driver waits out the kernel's TCP retry on a lost guest"
    end
  end
end
