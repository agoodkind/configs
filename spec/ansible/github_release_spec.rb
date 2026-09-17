# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# These checks render the release pull with ansible-core's own templar: the
# download URL of an archive, the directory it unpacks into, and the facts the
# play reads afterwards. They read the expressions from the real task file, so
# a change to any of them changes what is tested. The tasks that reach GitHub
# and the disk are not run; the fields they read are rendered instead.
module GithubRelease
  TASK_FILE = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'tasks', 'github-release.yml')
  ASSERT_KEY = 'ansible.builtin.assert'
  GET_URL_KEY = 'ansible.builtin.get_url'
  UNARCHIVE_KEY = 'ansible.builtin.unarchive'
  PIN_CONDITION = 'pin_accepted'
  PLAN_FACT = 'release_unpack_plan'
  SCRATCH_PREFIX = 'release_'
  STAGE_ROOT = '/controller/.make/releases'
  MWAN_REPO = 'agoodkind/configs'
  MWAN_TAG = '202609162332-86-8693b22'
  MWAN_COMMIT = '8693b22f892bc745fa16631e4b1fc874fb9ee53e'
  OPNSENSECTL_REPO = 'agoodkind/opnsensectl'
  OPNSENSECTL_TAG = '202609151528-1-a1fe548'
  OPNSENSECTL_COMMIT = 'a1fe5484c0c37b4097999412c607eeafd4c0a65a'
  CHECKSUM_PREFIX = 'sha256:'

  # The task file's expressions: the assert list that accepts or refuses the
  # pin, every set_fact task without a loop in file order, the looped set_fact
  # that plans one archive, and the download and unpack fields that read that
  # plan.
  Expressions = Struct.new(:pin, :facts, :plan, :download, :unpack, keyword_init: true)

  MWAN_ASSETS = [
    { 'name' => 'mwan_linux_amd64.tar.gz',
      'checksum' => "#{CHECKSUM_PREFIX}ddfe74f593d5ecf54f29c370d09135b106ea2684be27bed8c5159bee4a94b890" },
    { 'name' => 'wanconfig-stack_linux_amd64.tar.gz',
      'checksum' => "#{CHECKSUM_PREFIX}82beca3a786f1396431176de30c3752a6adcddbcbb89cd7f4e957ac94638516b" }
  ].freeze

  OPNSENSECTL_ASSETS = [
    { 'name' => 'opnsensectl_linux_amd64.tar.gz',
      'checksum' => "#{CHECKSUM_PREFIX}60d7e5eaa7aa8707fa66a19bb25922f2a80ce0c3b422f549f574651dbdb1da52" },
    { 'name' => 'opnsensectl_freebsd_amd64.tar.gz',
      'checksum' => "#{CHECKSUM_PREFIX}755be32a19fe0ff0a12b5d65b4828c5be59a14136e173850c1b706c2cac23132" }
  ].freeze

  # A pin is refused on the controller, before the play reaches a host, unless
  # its plan holds the binary archive and every archive carries a sha256
  # checksum. An empty pin would otherwise plan nothing while the release facts
  # are still set.
  PIN_CASES = [
    { name: 'the pinned mwan archives', prefix: 'mwan', assets: MWAN_ASSETS, want: true },
    { name: 'the pinned opnsensectl archives', prefix: 'opnsensectl', assets: OPNSENSECTL_ASSETS, want: true },
    { name: 'no archive', prefix: 'mwan', assets: [], want: false },
    { name: 'the data archive without the binary archive', prefix: 'mwan', assets: [MWAN_ASSETS.last], want: false },
    {
      name: 'a checksum without its algorithm', prefix: 'mwan',
      assets: [{ 'name' => MWAN_ASSETS.first['name'], 'checksum' => MWAN_ASSETS.first['checksum'].delete_prefix(CHECKSUM_PREFIX) }],
      want: false
    }
  ].freeze

  # Each archive carries the digest its release's checksums.txt publishes for
  # it, so the checksum that reaches the download is the one pinned for that
  # archive and not another archive's.
  ARCHIVE_CASES = [
    {
      name: 'the binary archive', prefix: 'mwan', repo: MWAN_REPO, tag: MWAN_TAG,
      archive: 'mwan_linux_amd64.tar.gz', want_dir: "#{STAGE_ROOT}/mwan/#{MWAN_TAG}/linux_amd64",
      digest: 'ddfe74f593d5ecf54f29c370d09135b106ea2684be27bed8c5159bee4a94b890'
    },
    {
      name: 'a data archive', prefix: 'mwan', repo: MWAN_REPO, tag: MWAN_TAG,
      archive: 'wanconfig-stack_linux_amd64.tar.gz', want_dir: "#{STAGE_ROOT}/mwan/#{MWAN_TAG}/wanconfig-stack",
      digest: '82beca3a786f1396431176de30c3752a6adcddbcbb89cd7f4e957ac94638516b'
    },
    {
      name: 'the binary archive of another platform', prefix: 'opnsensectl', repo: OPNSENSECTL_REPO, tag: OPNSENSECTL_TAG,
      archive: 'opnsensectl_freebsd_amd64.tar.gz', want_dir: "#{STAGE_ROOT}/opnsensectl/#{OPNSENSECTL_TAG}/freebsd_amd64",
      digest: '755be32a19fe0ff0a12b5d65b4828c5be59a14136e173850c1b706c2cac23132'
    }
  ].freeze

  FACT_CASES = [
    {
      name: 'mwan', prefix: 'mwan', repo: MWAN_REPO, tag: MWAN_TAG, commit: MWAN_COMMIT,
      want: {
        'mwan_release_tag' => MWAN_TAG,
        'mwan_release_commit' => MWAN_COMMIT,
        'mwan_release_dir' => "#{STAGE_ROOT}/mwan/#{MWAN_TAG}",
        'wanconfig_stack_dir' => "#{STAGE_ROOT}/mwan/#{MWAN_TAG}/wanconfig-stack"
      }
    },
    {
      name: 'opnsensectl', prefix: 'opnsensectl', repo: OPNSENSECTL_REPO, tag: OPNSENSECTL_TAG, commit: OPNSENSECTL_COMMIT,
      want: {
        'opnsensectl_release_tag' => OPNSENSECTL_TAG,
        'opnsensectl_release_commit' => OPNSENSECTL_COMMIT,
        'opnsensectl_release_dir' => "#{STAGE_ROOT}/opnsensectl/#{OPNSENSECTL_TAG}"
      }
    }
  ].freeze

  module_function

  def read_expressions
    expressions = Expressions.new(pin: nil, facts: [], plan: nil, download: nil, unpack: nil)
    collect(expressions, YAML.safe_load_file(TASK_FILE))
    raise "#{TASK_FILE} has no assert task with a that list" if expressions.pin.nil? || expressions.pin.empty?
    raise "#{TASK_FILE} has no looped set_fact task building #{PLAN_FACT}" if expressions.plan.nil?
    raise "#{TASK_FILE} has no get_url task" if expressions.download.nil?
    raise "#{TASK_FILE} has no unarchive task" if expressions.unpack.nil?
    raise "#{TASK_FILE} has no set_fact task naming the release for the play" if expressions.facts.empty?

    expressions
  end

  def collect(expressions, tasks)
    tasks.each do |task|
      set_fact = task[TaskExpressions::SET_FACT_KEY]
      if !task[ASSERT_KEY].nil?
        expressions.pin = TaskExpressions.condition_list(task[ASSERT_KEY]['that'])
      elsif !set_fact.nil? && !task['loop'].nil?
        expressions.plan = TaskExpressions.render_task(task, set_fact)
      elsif !set_fact.nil?
        expressions.facts << TaskExpressions.fact_task(task)
      elsif !task[GET_URL_KEY].nil?
        expressions.download = TaskExpressions.render_task(task, task[GET_URL_KEY].slice('url', 'dest', 'checksum'))
      elsif !task[UNARCHIVE_KEY].nil?
        expressions.unpack = TaskExpressions.render_task(task, task[UNARCHIVE_KEY].slice('src', 'dest', 'creates'))
      end
      collect(expressions, task['block'] || [])
    end
  end

  # The variables the importing play passes plus the one registered result the
  # facts read.
  def base_variables(prefix:, repo:, tag:, commit:)
    {
      'release_var_prefix' => prefix,
      'release_repo' => repo,
      'release_tag' => tag,
      'release_stage_root' => STAGE_ROOT,
      'release_commit_lookup' => TaskExpressions.command_result(0, commit, '')
    }
  end

  # The verdict of the assert that accepts or refuses a pin, evaluated against
  # the plan the looped set_fact builds from the pinned archives, one entry per
  # archive in pin order.
  def pin_accepted?(expressions, variables, assets)
    plan = assets.map { |asset| plan_entry(expressions, variables, asset['name'], asset['checksum']) }
    result = TaskExpressions.evaluate(
      variables: variables.merge(PLAN_FACT => plan),
      facts: [],
      conditions: { PIN_CONDITION => expressions.pin }
    )
    verdicts = result['conditions'] || {}
    raise "evaluator returned no #{PIN_CONDITION} verdict: #{result.inspect}" unless verdicts.key?(PIN_CONDITION)

    verdicts[PIN_CONDITION]
  end

  # The plan entry the looped set_fact appends for one archive.
  def plan_entry(expressions, variables, archive, checksum)
    plan_variables = variables.merge('item' => { 'name' => archive, 'checksum' => checksum })
    result = TaskExpressions.evaluate(variables: plan_variables, facts: expressions.facts, renders: [expressions.plan])
    plan = result['renders'].first.fetch(PLAN_FACT)
    raise "#{PLAN_FACT} = #{plan.inspect}, want a list of one entry" unless plan.is_a?(Array) && plan.size == 1

    plan.first
  end

  # The download and unpack fields rendered for one plan entry.
  def archive_fields(expressions, variables, entry)
    result = TaskExpressions.evaluate(
      variables: variables.merge('item' => entry),
      facts: expressions.facts,
      renders: [expressions.download, expressions.unpack]
    )
    { download: result['renders'][0], unpack: result['renders'][1] }
  end

  # The facts the play reads, without this file's scratch facts.
  def play_facts(expressions, variables)
    result = TaskExpressions.evaluate(variables: variables, facts: expressions.facts)
    result['facts'].reject { |name, _value| name.start_with?(SCRATCH_PREFIX) }
  end
end

RSpec.describe GithubRelease do
  before(:all) do
    @expressions = described_class.read_expressions
  end

  GithubRelease::PIN_CASES.each do |test_case|
    it "#{test_case[:want] ? 'accepts' : 'refuses'} a pin of #{test_case[:name]}" do
      variables = described_class.base_variables(
        prefix: test_case[:prefix], repo: GithubRelease::MWAN_REPO, tag: GithubRelease::MWAN_TAG, commit: GithubRelease::MWAN_COMMIT
      )
      accepted = described_class.pin_accepted?(@expressions, variables, test_case[:assets])

      expect(accepted).to be(test_case[:want]),
                          "pin accepted = #{accepted}, want #{test_case[:want]} (that #{@expressions.pin.inspect}, assets #{test_case[:assets].inspect})"
    end
  end

  GithubRelease::ARCHIVE_CASES.each do |test_case|
    it "downloads and unpacks #{test_case[:name]}", :aggregate_failures do
      variables = described_class.base_variables(
        prefix: test_case[:prefix], repo: test_case[:repo], tag: test_case[:tag], commit: GithubRelease::MWAN_COMMIT
      )
      checksum = "#{GithubRelease::CHECKSUM_PREFIX}#{test_case[:digest]}"
      entry = described_class.plan_entry(@expressions, variables, test_case[:archive], checksum)
      want_archive = "#{GithubRelease::STAGE_ROOT}/#{test_case[:prefix]}/#{test_case[:tag]}/#{test_case[:archive]}"
      want_url = "https://github.com/#{test_case[:repo]}/releases/download/#{test_case[:tag]}/#{test_case[:archive]}"

      expect(entry['dir']).to eq(test_case[:want_dir]), "unpack dir = #{entry['dir'].inspect}, want #{test_case[:want_dir].inspect}"
      expect(entry['archive']).to eq(want_archive), "archive = #{entry['archive'].inspect}, want #{want_archive.inspect}"
      expect(entry['checksum']).to eq(checksum), "checksum = #{entry['checksum'].inspect}, want the pinned #{checksum.inspect}"

      fields = described_class.archive_fields(@expressions, variables, entry)
      expect(fields[:download]['url']).to eq(want_url), "url = #{fields[:download]['url'].inspect}, want #{want_url.inspect}"
      expect(fields[:download]['dest']).to eq(want_archive), "download dest = #{fields[:download]['dest'].inspect}, want #{want_archive.inspect}"
      expect(fields[:download]['checksum']).to eq(checksum),
                                               "download checksum = #{fields[:download]['checksum'].inspect}, want the pinned #{checksum.inspect}"
      expect(fields[:unpack]['src']).to eq(want_archive), "unpack src = #{fields[:unpack]['src'].inspect}, want #{want_archive.inspect}"
      expect(fields[:unpack]['dest']).to eq(test_case[:want_dir]), "unpack dest = #{fields[:unpack]['dest'].inspect}, want #{test_case[:want_dir].inspect}"
      want_marker = "#{test_case[:want_dir]}/.unpacked-#{test_case[:digest]}"
      expect(fields[:unpack]['creates']).to eq(want_marker),
                                            "unpack creates = #{fields[:unpack]['creates'].inspect}, want the checksum marker #{want_marker.inspect}"
    end
  end

  GithubRelease::FACT_CASES.each do |test_case|
    it "names the #{test_case[:name]} release for the play" do
      variables = described_class.base_variables(
        prefix: test_case[:prefix], repo: test_case[:repo], tag: test_case[:tag], commit: test_case[:commit]
      )
      facts = described_class.play_facts(@expressions, variables)

      expect(facts).to eq(test_case[:want]), "facts = #{facts.inspect}, want exactly #{test_case[:want].inspect}"
    end
  end
end
