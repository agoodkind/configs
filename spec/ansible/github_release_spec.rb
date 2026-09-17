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
  GET_URL_KEY = 'ansible.builtin.get_url'
  UNARCHIVE_KEY = 'ansible.builtin.unarchive'
  PLAN_FACT = 'release_unpack_plan'
  SCRATCH_PREFIX = 'release_'
  STAGE_ROOT = '/controller/.make/releases'
  MWAN_REPO = 'agoodkind/configs'
  MWAN_TAG = '202609162332-86-8693b22'
  MWAN_COMMIT = '8693b22f892bc745fa16631e4b1fc874fb9ee53e'
  OPNSENSECTL_REPO = 'agoodkind/opnsensectl'
  OPNSENSECTL_TAG = '202609151528-1-a1fe548'
  OPNSENSECTL_COMMIT = 'a1fe5484c0c37b4097999412c607eeafd4c0a65a'
  CHECKSUM_DIGEST = 'ddfe74f593d5ecf54f29c370d09135b106ea2684be27bed8c5159bee4a94b890'
  CHECKSUM = "sha256:#{CHECKSUM_DIGEST}"

  # The task file's expressions: every set_fact task without a loop in file
  # order, the looped set_fact that plans one archive, and the download and
  # unpack fields that read that plan.
  Expressions = Struct.new(:facts, :plan, :download, :unpack, keyword_init: true)

  ARCHIVE_CASES = [
    {
      name: 'the binary archive', prefix: 'mwan', repo: MWAN_REPO, tag: MWAN_TAG,
      archive: 'mwan_linux_amd64.tar.gz', want_dir: "#{STAGE_ROOT}/mwan/#{MWAN_TAG}/linux_amd64"
    },
    {
      name: 'a data archive', prefix: 'mwan', repo: MWAN_REPO, tag: MWAN_TAG,
      archive: 'wanconfig-stack_linux_amd64.tar.gz', want_dir: "#{STAGE_ROOT}/mwan/#{MWAN_TAG}/wanconfig-stack"
    },
    {
      name: 'the binary archive of another platform', prefix: 'opnsensectl', repo: OPNSENSECTL_REPO, tag: OPNSENSECTL_TAG,
      archive: 'opnsensectl_freebsd_amd64.tar.gz', want_dir: "#{STAGE_ROOT}/opnsensectl/#{OPNSENSECTL_TAG}/freebsd_amd64"
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
    expressions = Expressions.new(facts: [], plan: nil, download: nil, unpack: nil)
    collect(expressions, YAML.safe_load_file(TASK_FILE))
    raise "#{TASK_FILE} has no looped set_fact task building #{PLAN_FACT}" if expressions.plan.nil?
    raise "#{TASK_FILE} has no get_url task" if expressions.download.nil?
    raise "#{TASK_FILE} has no unarchive task" if expressions.unpack.nil?
    raise "#{TASK_FILE} has no set_fact task naming the release for the play" if expressions.facts.empty?

    expressions
  end

  def collect(expressions, tasks)
    tasks.each do |task|
      set_fact = task[TaskExpressions::SET_FACT_KEY]
      if !set_fact.nil? && !task['loop'].nil?
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

  # The plan entry the looped set_fact appends for one archive.
  def plan_entry(expressions, variables, archive)
    plan_variables = variables.merge('item' => { 'name' => archive, 'checksum' => CHECKSUM })
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

  GithubRelease::ARCHIVE_CASES.each do |test_case|
    it "downloads and unpacks #{test_case[:name]}", :aggregate_failures do
      variables = described_class.base_variables(
        prefix: test_case[:prefix], repo: test_case[:repo], tag: test_case[:tag], commit: GithubRelease::MWAN_COMMIT
      )
      entry = described_class.plan_entry(@expressions, variables, test_case[:archive])
      want_archive = "#{GithubRelease::STAGE_ROOT}/#{test_case[:prefix]}/#{test_case[:tag]}/#{test_case[:archive]}"
      want_url = "https://github.com/#{test_case[:repo]}/releases/download/#{test_case[:tag]}/#{test_case[:archive]}"

      expect(entry['dir']).to eq(test_case[:want_dir]), "unpack dir = #{entry['dir'].inspect}, want #{test_case[:want_dir].inspect}"
      expect(entry['archive']).to eq(want_archive), "archive = #{entry['archive'].inspect}, want #{want_archive.inspect}"
      expect(entry['checksum']).to eq(GithubRelease::CHECKSUM), "checksum = #{entry['checksum'].inspect}, want the pinned one"

      fields = described_class.archive_fields(@expressions, variables, entry)
      expect(fields[:download]['url']).to eq(want_url), "url = #{fields[:download]['url'].inspect}, want #{want_url.inspect}"
      expect(fields[:download]['dest']).to eq(want_archive), "download dest = #{fields[:download]['dest'].inspect}, want #{want_archive.inspect}"
      expect(fields[:download]['checksum']).to eq(GithubRelease::CHECKSUM),
                                               "download checksum = #{fields[:download]['checksum'].inspect}, want the pinned #{GithubRelease::CHECKSUM.inspect}"
      expect(fields[:unpack]['src']).to eq(want_archive), "unpack src = #{fields[:unpack]['src'].inspect}, want #{want_archive.inspect}"
      expect(fields[:unpack]['dest']).to eq(test_case[:want_dir]), "unpack dest = #{fields[:unpack]['dest'].inspect}, want #{test_case[:want_dir].inspect}"
      want_marker = "#{test_case[:want_dir]}/.unpacked-#{GithubRelease::CHECKSUM_DIGEST}"
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
