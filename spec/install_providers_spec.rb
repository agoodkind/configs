# frozen_string_literal: true

require 'digest'
require 'fileutils'
require 'rubygems/package'
require 'tmpdir'
require 'zlib'
require_relative 'support/command_runner'

# The module provides helpers to prepare temporary pin files and archives and run the script.
module InstallProviders
  REPOSITORY_ROOT = File.expand_path('..', __dir__)
  SCRIPT = File.join(REPOSITORY_ROOT, 'opentofu', 'guest', 'install-providers.sh')
  SOURCE_ADDRESS = 'tofu.test/acme/example'
  REPOSITORY = 'acme/terraform-provider-example'
  TAG = 'test-tag'
  VERSION = '1.2.3'
  BINARY_NAME = 'terraform-provider-example'
  BINARY_CONTENT = "#!/bin/sh\nexit 0\n"
  PLATFORMS = %w[darwin_arm64 darwin_amd64 linux_amd64 linux_arm64].freeze
  WRONG_SHA256 = '0' * 64
  TIMEOUT_SECONDS = 60

  # The method returns the archive SHA-256.
  def self.write_archive(path)
    FileUtils.mkdir_p(File.dirname(path))
    Zlib::GzipWriter.open(path) do |gzip|
      Gem::Package::TarWriter.new(gzip) do |tar|
        tar.add_file_simple(BINARY_NAME, 0o755, BINARY_CONTENT.bytesize) { |entry| entry.write(BINARY_CONTENT) }
      end
    end
    Digest::SHA256.file(path).hexdigest
  end

  # The script does not call gh because the seeded directory contains every required archive.
  def self.seed(work_directory, pinned_sha256: nil)
    rows = PLATFORMS.map do |platform|
      archive = File.join(work_directory, 'archives', BINARY_NAME, TAG, "#{BINARY_NAME}_#{platform}.tar.gz")
      archive_sha256 = write_archive(archive)
      [SOURCE_ADDRESS, REPOSITORY, TAG, VERSION, platform, pinned_sha256 || archive_sha256].join(' ')
    end
    File.write(File.join(work_directory, 'providers.pin'), "#{rows.join("\n")}\n")
  end

  def self.run(work_directory)
    argv = ['bash', SCRIPT, File.join(work_directory, 'providers.pin'), File.join(work_directory, 'mirror'),
            File.join(work_directory, 'archives')]
    CommandRunner.run({}, argv, chdir: work_directory, timeout_seconds: TIMEOUT_SECONDS)
  end

  def self.installed(work_directory)
    Dir.glob(File.join(work_directory, 'mirror', SOURCE_ADDRESS, VERSION, '*', "#{BINARY_NAME}_v#{VERSION}"))
  end
end

RSpec.describe InstallProviders do
  around do |example|
    Dir.mktmpdir('install-providers') do |work_directory|
      @work_directory = work_directory
      example.run
    end
  end

  it 'installs the executable provider binary for the host platform at the mirror path', :aggregate_failures do
    described_class.seed(@work_directory)
    result = described_class.run(@work_directory)
    installed = described_class.installed(@work_directory)

    expect(result.exit_status.exitstatus).to eq(0), result.output
    expect(installed.length).to eq(1)
    expect(File.read(installed.first)).to eq(InstallProviders::BINARY_CONTENT)
    expect(File.executable?(installed.first)).to be(true)
  end

  it 'rejects a SHA-256 mismatch with status 1 without installing a provider binary', :aggregate_failures do
    described_class.seed(@work_directory, pinned_sha256: InstallProviders::WRONG_SHA256)
    result = described_class.run(@work_directory)

    expect(result.exit_status.exitstatus).to eq(1), result.output
    expect(result.output).to include("expected_sha256=#{InstallProviders::WRONG_SHA256}")
    expect(described_class.installed(@work_directory)).to be_empty
  end
end
