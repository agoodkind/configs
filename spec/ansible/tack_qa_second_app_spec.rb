# frozen_string_literal: true

require 'fileutils'
require 'tmpdir'
require 'yaml'
require_relative '../support/tack_search_inventory'

# These examples render the second QA application process (TACK-544) from the
# real inventory. They check the QA owner's audit broker listener and firewall,
# the application-only guest's settings, and that no production Tack guest
# renders the external listener, the firewall table, or an application-only
# start.
module TackQaSecondApp
  QA_OWNER = 'tack-qa.suburban.goodkind.io'
  QA_APP_GUEST = 'tack-app2.suburban.goodkind.io'
  QA_APP_GUEST_ADDRESS = '3d06:bad:b01:210::223'
  QA_OWNER_ADDRESS = '3d06:bad:b01:210::217'
  EXTERNAL_PORT = 9094
  BRIDGE_BROKER = 'kafka:9092'
  PRODUCTION_APP_GUEST = 'tack-app2.home.goodkind.io'
  PRODUCTION_HOSTS = %w[
    tack.home.goodkind.io tack-app2.home.goodkind.io tack-data1.home.goodkind.io
    tack-data2.home.goodkind.io tack-data3.home.goodkind.io tack-search1.home.goodkind.io
  ].freeze
  # The flags template prints the values deploy-tack.yml reads to decide
  # whether a guest installs the tack_queue firewall table and whether it runs
  # the application-only block.
  TEMPLATES = TackSearchInventory::TEMPLATES.merge(
    'queue_firewall' => 'tack/tack-queue-firewall.nft.j2',
    'flags' => 'spec/fixtures/ansible/tack_qa_second_app_flags.j2'
  ).freeze

  module_function

  # Renders the named templates for each host from the repository inventory
  # and returns the output directory.
  def render(host_templates)
    work_directory = Dir.mktmpdir('tack-qa-second-app')
    at_exit { FileUtils.remove_entry(work_directory) }
    inventory_directory = File.join(work_directory, 'inventory')
    FileUtils.mkdir_p(inventory_directory)
    TackSearchInventory::COPIED_ENTRIES.each do |entry|
      FileUtils.cp_r(File.join(TackSearchInventory::INVENTORY_DIRECTORY, entry), inventory_directory)
    end
    File.write(File.join(inventory_directory, TackSearchInventory::VAULT_FILE),
               YAML.dump(TackSearchInventory.placeholder_secrets))
    output_directory = File.join(work_directory, 'rendered')
    FileUtils.mkdir_p(output_directory)
    AnsibleRender.render(
      inventory: inventory_directory,
      playbook: TackSearchInventory::PLAYBOOK,
      extra_vars: {
        'render_hosts' => host_templates.keys.join(','),
        'render_templates' => host_templates.transform_values do |names|
          names.map { |name| { 'name' => name, 'src' => TEMPLATES.fetch(name) } }
        end,
        'repository_root' => AnsibleRender::REPOSITORY_ROOT,
        'output_directory' => output_directory,
        'tack_app_password' => TackSearchInventory::APP_PASSWORD,
        'ansible_become' => false
      }
    )
    output_directory
  end

  def production_render
    @production_render ||= render(PRODUCTION_HOSTS.to_h { |host| [host, %w[env override flags]] })
  end

  def qa_render
    @qa_render ||= render(QA_OWNER => %w[env override queue_firewall flags], QA_APP_GUEST => %w[env override flags])
  end

  def file(directory, host, name)
    File.read(File.join(directory, "#{host}-#{name}"))
  end

  def pairs(directory, host, name)
    file(directory, host, name).lines(chomp: true).grep(/\A[A-Za-z][A-Za-z0-9_]*=/).to_h { |line| line.split('=', 2) }
  end

  def published_ports(directory, host)
    services = YAML.safe_load(file(directory, host, 'override'), aliases: true).fetch('services', {}) || {}
    services.values.flat_map { |service| (service || {}).fetch('ports', []) }
  end
end

RSpec.describe TackQaSecondApp do
  TackQaSecondApp::PRODUCTION_HOSTS.each do |host|
    it "keeps production guest #{host} on the bridge broker without the external listener", :aggregate_failures do
      render = described_class.production_render
      flags = described_class.pairs(render, host, 'flags')

      expect(described_class.pairs(render, host, 'env')).to include('AUDIT_KAFKA_BROKERS' => TackQaSecondApp::BRIDGE_BROKER)
      expect(described_class.published_ports(render, host).grep(/#{TackQaSecondApp::EXTERNAL_PORT}/o)).to be_empty
      expect(described_class.file(render, host, 'override')).not_to include('EXTERNAL://')
      expect(flags).to include('tack_queue_external_listener_enabled' => 'false')
    end
  end

  it 'renders no application-only start for the production tack_app2 guest' do
    flags = described_class.pairs(described_class.production_render, TackQaSecondApp::PRODUCTION_APP_GUEST, 'flags')

    expect(flags).to include('tack_app_guest' => 'false', 'tack_provision_owner' => 'false')
  end

  it 'gives the QA owner broker an external listener on its pinned address', :aggregate_failures do
    render = described_class.qa_render
    kafka = YAML.safe_load(described_class.file(render, TackQaSecondApp::QA_OWNER, 'override')).dig('services', 'kafka')
    port = TackQaSecondApp::EXTERNAL_PORT

    expect(described_class.pairs(render, TackQaSecondApp::QA_OWNER, 'flags')).to include(
      'tack_provision_owner' => 'true', 'tack_queue_external_listener_enabled' => 'true'
    )
    expect(kafka.fetch('ports')).to eq(["#{port}:#{port}"])
    expect(kafka.fetch('environment')).to include(
      'KAFKA_LISTENERS' => "PLAINTEXT://[::]:9092,CONTROLLER://[::]:9093,EXTERNAL://[::]:#{port}",
      'KAFKA_ADVERTISED_LISTENERS' => "PLAINTEXT://kafka:9092,EXTERNAL://[#{TackQaSecondApp::QA_OWNER_ADDRESS}]:#{port}"
    )
  end

  it 'accepts the QA broker port only from the application-only guest in both hooks', :aggregate_failures do
    table = described_class.file(described_class.qa_render, TackQaSecondApp::QA_OWNER, 'queue_firewall')
    port = TackQaSecondApp::EXTERNAL_PORT

    expect(table).to include("elements = { #{TackQaSecondApp::QA_APP_GUEST_ADDRESS} }")
    %w[input forward].each do |hook|
      chain = table[/chain #{hook} \{.*?\n    \}/m]
      expect(chain).to include("type filter hook #{hook}")
      expect(chain).to include("tcp dport #{port} ip6 saddr @clients accept\n        tcp dport #{port} drop")
    end
  end

  it 'starts the QA application-only guest against the shared stores and the owner broker', :aggregate_failures do
    render = described_class.qa_render
    override = YAML.safe_load(described_class.file(render, TackQaSecondApp::QA_APP_GUEST, 'override'))
    app = override.dig('services', 'app')

    expect(described_class.pairs(render, TackQaSecondApp::QA_APP_GUEST, 'flags')).to include(
      'tack_app_guest' => 'true', 'tack_provision_owner' => 'false'
    )
    expect(override.fetch('services').keys).to eq(['app'])
    expect(app.fetch('extra_hosts')).to eq(%w[yb1:3d06:bad:b01:210::220 yb2:3d06:bad:b01:210::221 yb3:3d06:bad:b01:210::222])
    expect(app.dig('environment', 'DATABASE_URL')).to start_with('postgres://tack_app:${TACK_APP_PASSWORD:?}@yb1:5433,yb2:5433,yb3:5433/')
    expect(described_class.pairs(render, TackQaSecondApp::QA_APP_GUEST, 'env')).to include(
      'AUDIT_KAFKA_BROKERS' => "[#{TackQaSecondApp::QA_OWNER_ADDRESS}]:#{TackQaSecondApp::EXTERNAL_PORT}",
      'OPENSEARCH_PUBLIC_ENABLED' => 'true'
    )
    expect(described_class.pairs(render, TackQaSecondApp::QA_OWNER, 'env')).to include(
      'AUDIT_KAFKA_BROKERS' => TackQaSecondApp::BRIDGE_BROKER
    )
  end
end
