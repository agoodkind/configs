# frozen_string_literal: true

require 'yaml'
require_relative '../support/ansible_render'
require_relative '../support/task_expressions'

# These checks evaluate the slow storage tier migration's skip decisions with
# ansible-core's own templar (TACK-495). They read the set_fact tasks and the
# condition lists from the real task files and feed them pct config output in
# the shapes the hypervisors print, so a change to any parsing expression
# changes what is tested.
module SlowTierDecision
  TASK_DIRECTORY = File.join(AnsibleRender::REPOSITORY_ROOT, 'ansible', 'playbooks', 'tasks')
  MOVE_TASK_FILE = File.join(TASK_DIRECTORY, 'slow-tier-volume-move.yml')
  SCRATCH_TASK_FILE = File.join(TASK_DIRECTORY, 'slow-tier-scratch-mount.yml')
  MOVE_BLOCK_NAME = 'Move the volume onto the slow tier'
  MOVE_CONFIRM_NAME = 'Confirm the volume now lives on the slow tier'
  SCRATCH_BLOCK_NAME = 'Move the backup root onto a slow tier volume'
  SCRATCH_REFUSE_NAME = 'Refuse a backup root mounted from another storage'
  BACKUP_ROOT = '/root/backups'

  # Lines every case shares. The network line carries colons in its values, so
  # a parser that read any line but the volume's own would misread it.
  COMMON_LINES = [
    'arch: amd64',
    'cores: 2',
    'net0: name=eth0,bridge=vmbr0,gw6=3d06:bad:b01::1,hwaddr=BC:24:11:00:01:18,ip6=3d06:bad:b01::118/64,type=veth',
    'unprivileged: 1'
  ].freeze

  # The positional form vault printed for the object store on 2026-09-18, the
  # keyed form suburban printed for the QA owner guest, and each after a move.
  MOVE_CASES = [
    { name: 'a positional volume on the thin pool', storage: 'storage',
      rootfs: 'rootfs: local-lvm:vm-118-disk-0,size=100G', want_move: true },
    { name: 'a positional volume already on the tier', storage: 'storage',
      rootfs: 'rootfs: storage:118/vm-118-disk-0.raw,size=100G', want_move: false },
    { name: 'a positional volume on rpool', storage: 'slow-zfs',
      rootfs: 'rootfs: local-zfs:subvol-218-disk-0,size=100G', want_move: true },
    { name: 'a keyed volume on rpool', storage: 'slow-zfs',
      rootfs: 'rootfs: acl=0,size=42949672960,mountoptions=discard,quota=0,replicate=0,volume=local-zfs:subvol-217-disk-0',
      want_move: true },
    { name: 'a keyed volume already on the tier', storage: 'slow-zfs',
      rootfs: 'rootfs: acl=0,size=42949672960,quota=0,replicate=0,volume=slow-zfs:subvol-217-disk-0', want_move: false }
  ].freeze

  TIER_MOUNT = 'mp0: storage:117/vm-117-disk-1.raw,mp=/root/backups,backup=0,size=100G'
  ASIDE = '/root/backups.hot-pool'

  # want_mount is whether the move block runs; want_accept is the refusal
  # assert's verdict, which runs only when a volume is already mounted there.
  SCRATCH_CASES = [
    { name: 'no volume is mounted at the backup root', mount_lines: [], aside: '',
      want_mount: true, want_accept: nil },
    { name: 'a volume is mounted at another path',
      mount_lines: ['mp0: storage:117/vm-117-disk-1.raw,mp=/root/backups-old,backup=0,size=100G'], aside: '',
      want_mount: true, want_accept: nil },
    { name: 'a tier volume is mounted and the old files are gone', mount_lines: [TIER_MOUNT], aside: '',
      want_mount: false, want_accept: true },
    { name: 'a tier volume is mounted but the old files remain', mount_lines: [TIER_MOUNT], aside: ASIDE,
      want_mount: true, want_accept: true },
    { name: 'the old files were set aside before the mount failed', mount_lines: [], aside: ASIDE,
      want_mount: true, want_accept: nil },
    { name: 'a thin pool volume is mounted at the backup root',
      mount_lines: ['mp1: local-lvm:vm-117-disk-1,mp=/root/backups,size=100G'], aside: '',
      want_mount: false, want_accept: false }
  ].freeze

  # systemctl list-unit-files --type=timer --state=enabled --no-legend
  # 'tack-*.timer' as the QA owner guest printed it on 2026-09-19, with the
  # timers stopped: the unit files stay listed after a stop.
  TIMER_FILE_LINES = [
    'tack-backup-restore-drill.timer enabled enabled',
    'tack-backup-staleness.timer     enabled enabled',
    'tack-ledger-export.timer        enabled enabled'
  ].freeze
  TIMER_NAMES = %w[tack-backup-restore-drill.timer tack-backup-staleness.timer tack-ledger-export.timer].freeze

  POOL_TASK_FILE = File.join(TASK_DIRECTORY, 'proxmox-slow-zpool.yml')
  POOL_BLOCK_NAME = 'Build the slow tier pool'
  POOL_REFUSE_NAME = 'Refuse a device that an imported pool or a mount still uses'
  POOL_REGISTER_NAME = 'Register the slow tier pool as Proxmox guest storage'
  POOL_NAME = 'slowpool'
  STORAGE_ID = 'slow-zfs'

  # zpool status -LP as suburban prints it after the BX500 drives left rpool:
  # the remaining mirror members by kernel path, and the removed vdev.
  RPOOL_STATUS = <<~STATUS
      pool: rpool
     state: ONLINE
    config:

    	NAME           STATE     READ WRITE CKSUM
    	rpool          ONLINE       0     0     0
    	  mirror-0     ONLINE       0     0     0
    	    /dev/sda3  ONLINE       0     0     0
    	    /dev/sdb3  ONLINE       0     0     0

    errors: No known data errors
  STATUS

  PVESM_WITHOUT_TIER = [
    'Name             Type     Status     Total (KiB)      Used (KiB) Available (KiB)        %',
    'local             dir     active       271422336        43762560       227659776   16.12%',
    'local-zfs     zfspool     active       479627184       251967312       227659872   52.53%'
  ].freeze
  PVESM_WITH_TIER = (PVESM_WITHOUT_TIER + [
    'slow-zfs      zfspool     active       942000000              96       942000000    0.00%'
  ]).freeze

  DEVICE_CASES = [
    { name: 'a free BX500 partition', source: '/dev/sdc3', mounted: [], want_accept: true },
    { name: 'a partition rpool still uses', source: '/dev/sda3', mounted: [], want_accept: false },
    { name: 'a mounted partition', source: '/dev/sdc3', mounted: ['/dev/sdc3'], want_accept: false }
  ].freeze

  module_function

  def config_result(lines)
    TaskExpressions.command_result(0, lines.join("\n"), '').merge('stdout_lines' => lines)
  end

  def tasks_in(tasks)
    tasks.flat_map { |task| [task] + tasks_in(task['block'] || []) + tasks_in(task['always'] || []) }
  end

  def task_named(tasks, prefix, file)
    task = tasks.find { |candidate| candidate['name'].to_s.start_with?(prefix) }
    raise "#{file} has no task named #{prefix.inspect}" if task.nil?

    task
  end

  def fact_tasks(tasks)
    tasks.reject { |task| task[TaskExpressions::SET_FACT_KEY].nil? }.map { |task| TaskExpressions.fact_task(task) }
  end

  def read(file)
    tasks_in(YAML.safe_load_file(file))
  end
end

RSpec.describe SlowTierDecision do
  describe 'volume move' do
    before(:all) do
      @tasks = described_class.read(SlowTierDecision::MOVE_TASK_FILE)
      @facts = described_class.fact_tasks(@tasks)
      @move_when = TaskExpressions.condition_list(
        described_class.task_named(@tasks, SlowTierDecision::MOVE_BLOCK_NAME, SlowTierDecision::MOVE_TASK_FILE)['when']
      )
      @confirm_that = TaskExpressions.condition_list(
        described_class.task_named(@tasks, SlowTierDecision::MOVE_CONFIRM_NAME, SlowTierDecision::MOVE_TASK_FILE)
          .dig('ansible.builtin.assert', 'that')
      )
    end

    SlowTierDecision::MOVE_CASES.each do |test_case|
      it "decides the move for #{test_case[:name]}", :aggregate_failures do
        config = described_class.config_result(SlowTierDecision::COMMON_LINES + [test_case[:rootfs]])
        variables = {
          'proxmox_slow_storage' => test_case[:storage],
          'slow_tier_move' => { 'vmid' => 118, 'volume' => 'rootfs' },
          'slow_tier_move_config' => config,
          'slow_tier_move_config_after' => config
        }
        result = TaskExpressions.evaluate(
          variables: variables, facts: @facts, conditions: { 'move' => @move_when, 'confirm' => @confirm_that }
        )

        expect(result['conditions']['move']).to be(test_case[:want_move]),
                                                 "move = #{result['conditions']['move']}, facts #{result['facts']}"
        # The confirmation after a move reads the same line, so it passes
        # exactly when the volume already sits on the tier.
        expect(result['conditions']['confirm']).to be(!test_case[:want_move])
      end
    end
  end

  describe 'backup root mount' do
    before(:all) do
      @tasks = described_class.read(SlowTierDecision::SCRATCH_TASK_FILE)
      @facts = described_class.fact_tasks(@tasks)
      @mount_when = TaskExpressions.condition_list(
        described_class.task_named(@tasks, SlowTierDecision::SCRATCH_BLOCK_NAME, SlowTierDecision::SCRATCH_TASK_FILE)['when']
      )
      refuse_task = described_class.task_named(@tasks, SlowTierDecision::SCRATCH_REFUSE_NAME, SlowTierDecision::SCRATCH_TASK_FILE)
      @refuse_when = TaskExpressions.condition_list(refuse_task['when'])
      @refuse_that = TaskExpressions.condition_list(refuse_task.dig('ansible.builtin.assert', 'that'))
    end

    SlowTierDecision::SCRATCH_CASES.each do |test_case|
      it "decides the mount when #{test_case[:name]}", :aggregate_failures do
        variables = {
          'proxmox_slow_storage' => 'storage',
          'slow_tier_scratch' => { 'vmid' => 117, 'key' => 'mp0', 'path' => SlowTierDecision::BACKUP_ROOT, 'size_gib' => 100 },
          'slow_tier_scratch_config' => described_class.config_result(SlowTierDecision::COMMON_LINES + test_case[:mount_lines]),
          'slow_tier_scratch_aside' => TaskExpressions.command_result(0, test_case[:aside], ''),
          'slow_tier_scratch_timer_files' => described_class.config_result(SlowTierDecision::TIMER_FILE_LINES)
        }
        conditions = { 'mount' => @mount_when, 'refuse_runs' => @refuse_when }
        conditions['accept'] = @refuse_that unless test_case[:want_accept].nil?
        result = TaskExpressions.evaluate(variables: variables, facts: @facts, conditions: conditions)

        expect(result['conditions']['mount']).to be(test_case[:want_mount]),
                                                  "mount = #{result['conditions']['mount']}, facts #{result['facts']}"
        # The pause and resume name these timers; a glob would start nothing
        # once the stop unloaded them.
        expect(result['facts']['slow_tier_scratch_timers']).to eq(SlowTierDecision::TIMER_NAMES)
        expect(result['conditions']['refuse_runs']).to be(!test_case[:want_accept].nil?)
        expect(result['conditions']['accept']).to be(test_case[:want_accept]) unless test_case[:want_accept].nil?
      end
    end
  end

  describe 'slow tier pool' do
    before(:all) do
      file = SlowTierDecision::POOL_TASK_FILE
      @tasks = described_class.read(file)
      @build_when = TaskExpressions.condition_list(described_class.task_named(@tasks, SlowTierDecision::POOL_BLOCK_NAME, file)['when'])
      @refuse_that = TaskExpressions.condition_list(
        described_class.task_named(@tasks, SlowTierDecision::POOL_REFUSE_NAME, file).dig('ansible.builtin.assert', 'that')
      )
      @register_when = TaskExpressions.condition_list(described_class.task_named(@tasks, SlowTierDecision::POOL_REGISTER_NAME, file)['when'])
    end

    def pool_variables(imported_pools, storage_lines)
      {
        'proxmox_slow_zpool_name' => SlowTierDecision::POOL_NAME,
        'proxmox_slow_storage' => SlowTierDecision::STORAGE_ID,
        'slow_zpool_imported' => described_class.config_result(imported_pools),
        'slow_zpool_storages' => described_class.config_result(storage_lines)
      }
    end

    it 'builds and registers the pool only while each is absent', :aggregate_failures do
      conditions = { 'build' => @build_when, 'register' => @register_when }
      fresh = TaskExpressions.evaluate(variables: pool_variables(['rpool'], SlowTierDecision::PVESM_WITHOUT_TIER), facts: [], conditions: conditions)
      done = TaskExpressions.evaluate(
        variables: pool_variables(['rpool', SlowTierDecision::POOL_NAME], SlowTierDecision::PVESM_WITH_TIER), facts: [], conditions: conditions
      )

      expect(fresh['conditions']).to eq('build' => true, 'register' => true)
      expect(done['conditions']).to eq('build' => false, 'register' => false)
    end

    SlowTierDecision::DEVICE_CASES.each do |test_case|
      it "decides whether to clear the label of #{test_case[:name]}" do
        variables = {
          'item' => { 'item' => '/dev/disk/by-id/example-part3',
                      'stat' => { 'exists' => true, 'islnk' => true, 'lnk_source' => test_case[:source] } },
          'slow_zpool_members' => described_class.config_result(SlowTierDecision::RPOOL_STATUS.lines(chomp: true)),
          'slow_zpool_mounted' => described_class.config_result(test_case[:mounted])
        }
        result = TaskExpressions.evaluate(variables: variables, facts: [], conditions: { 'accept' => @refuse_that })

        expect(result['conditions']['accept']).to be(test_case[:want_accept])
      end
    end
  end
end
