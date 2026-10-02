# frozen_string_literal: true

require 'fileutils'
require 'json'
require 'open3'
require 'tmpdir'

# The objset recorder reads ZFS objset kstat files and appends per-dataset
# write deltas. These checks run the real script under bash against kstat
# files in the format /proc/spl/kstat/zfs/<pool>/objset-* uses.
module StorageRecordingObjset
  REPOSITORY_ROOT = File.expand_path('..', __dir__)
  SCRIPT = File.join(REPOSITORY_ROOT, 'proxmox', 'scripts', 'storage-recording-objset.sh')
  POOL = 'rpool'
  DATASET = 'rpool/subvol-217-disk-0'

  # Writes one objset kstat file in the kernel's three-column format.
  def self.write_objset(kstat_directory, name, writes:, nwritten:)
    pool_directory = File.join(kstat_directory, POOL)
    FileUtils.mkdir_p(pool_directory)
    File.write(File.join(pool_directory, name), <<~KSTAT)
      35 1 0x01 7 2160 5214395032 4129624733823
      name                            type data
      dataset_name                    7    #{DATASET}
      writes                          4    #{writes}
      nwritten                        4    #{nwritten}
      reads                           4    100
      nread                           4    4096
    KSTAT
  end

  def self.run(work_directory)
    Open3.capture2e('bash', SCRIPT, File.join(work_directory, 'objset.state'),
                    File.join(work_directory, 'objset-writes.jsonl'), File.join(work_directory, 'kstat'), POOL)
  end

  def self.lines(work_directory)
    File.readlines(File.join(work_directory, 'objset-writes.jsonl')).map { |line| JSON.parse(line) }
  end
end

RSpec.describe StorageRecordingObjset do
  around do |example|
    Dir.mktmpdir('storage-recording-objset') do |work_directory|
      @work_directory = work_directory
      example.run
    end
  end

  # Pins the per-minute contract: the first run records counters with null
  # deltas, and the next run records the change since the first.
  it 'records the write delta since the previous run' do
    kstat = File.join(@work_directory, 'kstat')
    StorageRecordingObjset.write_objset(kstat, 'objset-0x36', writes: 1000, nwritten: 40_960)
    output, status = StorageRecordingObjset.run(@work_directory)
    expect(status.exitstatus).to eq(0), output

    StorageRecordingObjset.write_objset(kstat, 'objset-0x36', writes: 1250, nwritten: 57_344)
    output, status = StorageRecordingObjset.run(@work_directory)
    expect(status.exitstatus).to eq(0), output

    first, second = StorageRecordingObjset.lines(@work_directory)
    expect(first).to include('dataset' => StorageRecordingObjset::DATASET, 'writes' => 1000,
                             'writes_delta' => nil, 'nwritten_delta' => nil)
    expect(second).to include('dataset' => StorageRecordingObjset::DATASET, 'writes' => 1250,
                              'writes_delta' => 250, 'nwritten_delta' => 16_384, 'reads_delta' => 0)
  end

  # Pins that a counter lower than before (the pool was imported again) gives
  # a null delta rather than a negative one.
  it 'records a null delta after a counter reset' do
    kstat = File.join(@work_directory, 'kstat')
    StorageRecordingObjset.write_objset(kstat, 'objset-0x36', writes: 1000, nwritten: 40_960)
    StorageRecordingObjset.run(@work_directory)
    StorageRecordingObjset.write_objset(kstat, 'objset-0x36', writes: 10, nwritten: 4096)
    output, status = StorageRecordingObjset.run(@work_directory)
    expect(status.exitstatus).to eq(0), output

    expect(StorageRecordingObjset.lines(@work_directory).last).to include('writes' => 10, 'writes_delta' => nil)
  end

  # Pins the loud failure for a pool with no kstat directory.
  it 'fails for a pool with no kstat directory' do
    FileUtils.mkdir_p(File.join(@work_directory, 'kstat'))
    output, status = StorageRecordingObjset.run(@work_directory)

    expect(status.exitstatus).to eq(1), output
    expect(output).to include('no kstat directory')
  end
end
