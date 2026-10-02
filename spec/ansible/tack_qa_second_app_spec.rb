# frozen_string_literal: true

require 'fileutils'
require 'tmpdir'
require 'yaml'
require_relative '../support/tack_search_inventory'

# These examples render the second QA application process (TACK-544) from the
# real inventory. They check the QA owner's audit broker listener and firewall,
# the application-only guest's settings, and that every production Tack guest
# renders the same bytes as the merge base with origin/main.
module TackQaSecondApp
  BASE_REF_VARIABLE = 'TACK_RENDER_BASE_REF'
  DEFAULT_BASE_REF = 'refs/remotes/origin/main'
  GIT_TIMEOUT_SECONDS = 120
  RENDERED_TREES = %w[ansible/inventory tack].freeze
  QA_OWNER = 'tack-qa.suburban.goodkind.io'
  QA_APP_GUEST = 'tack-app2.suburban.goodkind.io'
  QA_APP_GUEST_ADDRESS = '3d06:bad:b01:210::223'
  QA_OWNER_ADDRESS = '3d06:bad:b01:210::217'
  EXTERNAL_PORT = 9094
  PRODUCTION_HOSTS = %w[
    tack.home.goodkind.io tack-app2.home.goodkind.io tack-data1.home.goodkind.io
    tack-data2.home.goodkind.io tack-data3.home.goodkind.io tack-search1.home.goodkind.io
  ].freeze
  TEMPLATES = TackSearchInventory::TEMPLATES.merge('queue_firewall' => 'tack/tack-queue-firewall.nft.j2').freeze

  module_function

  def templates_for(names)
    names.map { |name| { 'name' => name, 'src' => TEMPLATES.fetch(name) } }
  end

  # Renders the named templates for each host from the inventory and
  # templates under tree_root, and returns the output directory.
  def render(tree_root, host_templates)
    work_directory = Dir.mktmpdir('tack-qa-second-app')
    at_exit { FileUtils.remove_entry(work_directory) }
    inventory_directory = File.join(work_directory, 'inventory')
    FileUtils.mkdir_p(inventory_directory)
    TackSearchInventory::COPIED_ENTRIES.each do |entry|
      FileUtils.cp_r(File.join(tree_root, 'ansible', 'inventory', entry), inventory_directory)
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
        'render_templates' => host_templates.transform_values { |names| templates_for(names) },
        'repository_root' => tree_root,
        'output_directory' => output_directory,
        'tack_app_password' => TackSearchInventory::APP_PASSWORD,
        'ansible_become' => false
      }
    )
    output_directory
  end

  def git(*arguments)
    result = CommandRunner.run({}, ['git', *arguments], chdir: AnsibleRender::REPOSITORY_ROOT,
                                                        timeout_seconds: GIT_TIMEOUT_SECONDS)
    raise "git #{arguments.join(' ')} exceeded #{GIT_TIMEOUT_SECONDS}s" if result.timed_out
    raise "git #{arguments.join(' ')}: #{result.exit_status}\n#{result.output}" unless result.exit_status.success?

    result.output.strip
  end

  # CI checks out one commit with no history. The fetch adds the base branch
  # and the history that merge-base needs.
  def merge_base
    base_ref = ENV.fetch(BASE_REF_VARIABLE, DEFAULT_BASE_REF)
    if git('rev-parse', '--is-shallow-repository') == 'true'
      git('fetch', '--no-tags', '--unshallow', 'origin', "+refs/heads/main:#{DEFAULT_BASE_REF}")
    elsif !system('git', 'rev-parse', '--verify', '--quiet', "#{base_ref}^{commit}",
                  chdir: AnsibleRender::REPOSITORY_ROOT, out: File::NULL)
      git('fetch', '--no-tags', 'origin', "+refs/heads/main:#{DEFAULT_BASE_REF}")
    end
    git('merge-base', 'HEAD', base_ref)
  end

  def base_tree
    @base_tree ||= begin
      tree_root = Dir.mktmpdir('tack-qa-second-app-base')
      at_exit { FileUtils.remove_entry(tree_root) }
      archive = File.join(tree_root, 'base.tar')
      git('archive', '--format=tar', "--output=#{archive}", merge_base, *RENDERED_TREES)
      system('tar', '-xf', archive, '-C', tree_root, exception: true)
      tree_root
    end
  end

  def production_templates
    PRODUCTION_HOSTS.to_h { |host| [host, %w[env override]] }
  end

  def production_render(tree_root)
    @production_render ||= {}
    @production_render[tree_root] ||= render(tree_root, production_templates)
  end

  def qa_render
    @qa_render ||= render(AnsibleRender::REPOSITORY_ROOT,
                          QA_OWNER => %w[env override queue_firewall], QA_APP_GUEST => %w[env override])
  end

  def file(directory, host, name)
    File.read(File.join(directory, "#{host}-#{name}"))
  end

  def settings(directory, host)
    file(directory, host, 'env').lines(chomp: true).grep(/\A[A-Z][A-Z0-9_]*=/).to_h { |line| line.split('=', 2) }
  end
end

RSpec.describe TackQaSecondApp do
  TackQaSecondApp::PRODUCTION_HOSTS.product(%w[env override]).each do |host, name|
    it "renders the production #{name} for #{host} byte-identical to the merge base" do
      branch = described_class.file(described_class.production_render(AnsibleRender::REPOSITORY_ROOT), host, name)
      base = described_class.file(described_class.production_render(described_class.base_tree), host, name)

      expect(branch).to eq(base)
    end
  end

  it 'gives the QA owner broker an external listener on its pinned address' do
    kafka = YAML.safe_load(described_class.file(described_class.qa_render, TackQaSecondApp::QA_OWNER, 'override'))
                .dig('services', 'kafka')
    port = TackQaSecondApp::EXTERNAL_PORT

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

    expect(override.fetch('services').keys).to eq(['app'])
    expect(app.fetch('extra_hosts')).to eq(%w[yb1:3d06:bad:b01:210::220 yb2:3d06:bad:b01:210::221 yb3:3d06:bad:b01:210::222])
    expect(app.dig('environment', 'DATABASE_URL')).to start_with('postgres://tack_app:${TACK_APP_PASSWORD:?}@yb1:5433,yb2:5433,yb3:5433/')
    expect(described_class.settings(render, TackQaSecondApp::QA_APP_GUEST)).to include(
      'AUDIT_KAFKA_BROKERS' => "[#{TackQaSecondApp::QA_OWNER_ADDRESS}]:#{TackQaSecondApp::EXTERNAL_PORT}",
      'OPENSEARCH_PUBLIC_ENABLED' => 'false'
    )
    expect(described_class.settings(render, TackQaSecondApp::QA_OWNER)).to include('AUDIT_KAFKA_BROKERS' => 'kafka:9092')
  end
end
