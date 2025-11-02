# frozen_string_literal: true

require 'rspec/core/rake_task'
require 'rubocop/rake_task'
require 'fileutils'
require 'dotenv/load'

##
# Logstash Filterlog Parser Rakefile
#
# This Rakefile provides comprehensive deployment and management tasks for
# a custom Logstash Ruby filter and pipeline configurations. It supports
# local, remote (SSH), and Proxmox LXC container deployments.
#
# @author Alex Goodkind
# @version 1.0

# Configuration
RUBY_DIR = 'ruby'
CONF_DIR = 'conf'
LOGSTASH_RUBY_DIR = '/etc/logstash/ruby'
LOGSTASH_CONF_DIR = '/etc/logstash/conf.d'
LOGSTASH_USER = ENV.fetch('LOGSTASH_USER', 'logstash')
LOGSTASH_GROUP = ENV.fetch('LOGSTASH_GROUP', 'logstash')
SYSLOG_PORT = ENV.fetch('SYSLOG_PORT', '5140').to_i
REMOTE_HOST = ENV.fetch('REMOTE_HOST', 'root@logstash.home.goodkind.io')
PROXMOX_HOST = ENV.fetch('PROXMOX_HOST', nil)
PROXMOX_VMID = ENV.fetch('PROXMOX_VMID', nil)

##
# String class extensions for colored terminal output
class String
  # @return [String] the string wrapped in ANSI green color codes
  def green
    "\e[32m#{self}\e[0m"
  end

  # @return [String] the string wrapped in ANSI yellow color codes
  def yellow
    "\e[33m#{self}\e[0m"
  end

  # @return [String] the string wrapped in ANSI blue color codes
  def blue
    "\e[34m#{self}\e[0m"
  end

  # @return [String] the string wrapped in ANSI red color codes
  def red
    "\e[31m#{self}\e[0m"
  end
end

##
# Check if dry-run mode is enabled
#
# @return [Boolean] true if DRY_RUN environment variable is set to '1'
def dry_run?
  ENV['DRY_RUN'] == '1'
end

##
# Check if verbose mode is enabled
#
# @return [Boolean] true if VERBOSE environment variable is set to '1'
def verbose?
  ENV['VERBOSE'] == '1'
end

##
# Check if remote SSH deployment is enabled
#
# @return [Boolean] true if REMOTE environment variable is set to '1'
def remote_enabled?
  ENV['REMOTE'] == '1'
end

##
# Check if Proxmox container deployment is enabled
#
# @return [Boolean] true if both PROXMOX_HOST and PROXMOX_VMID are set
def proxmox_enabled?
  PROXMOX_HOST && PROXMOX_VMID
end

##
# Execute a command based on deployment mode (local, remote, or Proxmox)
#
# Routes command execution to the appropriate handler based on environment
# variables. Priority: Proxmox > Remote > Local
#
# @param cmd [String] the command to execute
# @return [void]
# @raise [RuntimeError] if command execution fails
def run_cmd(cmd)
  if proxmox_enabled?
    proxmox_cmd(cmd)
  elsif remote_enabled?
    remote_cmd(cmd)
  elsif dry_run?
    puts "[DRY-RUN] #{cmd}".yellow
  else
    sh cmd
  end
end

##
# Execute a command on a remote host via SSH
#
# @param cmd [String] the command to execute on the remote host
# @return [void]
# @raise [RuntimeError] if SSH command fails
def remote_cmd(cmd)
  full_cmd = "ssh #{REMOTE_HOST} '#{cmd}'"
  if dry_run?
    puts "[DRY-RUN] #{full_cmd}".yellow
  else
    sh full_cmd
  end
end

##
# Execute a command in a Proxmox LXC container
#
# Uses 'pct exec' to run commands inside the specified container on a
# Proxmox host via SSH. Creates a temporary shell script to avoid
# complex quoting/escaping issues through multiple SSH layers.
#
# @param cmd [String] the command to execute inside the container
# @return [void]
# @raise [RuntimeError] if pct exec command fails
def proxmox_cmd(cmd)
  if dry_run?
    puts "[DRY-RUN] pct exec #{PROXMOX_VMID} -- #{cmd}".yellow
    return
  end

  # Create temporary script to avoid escaping issues
  timestamp = Time.now.strftime('%Y%m%d_%H%M%S')
  local_script = "/tmp/logstash_cmd_#{timestamp}.sh"
  host_script = "/tmp/logstash_cmd_#{timestamp}.sh"
  container_script = "/tmp/logstash_cmd_#{timestamp}.sh"

  begin
    # Write command to local temp script
    File.write(local_script, "#!/bin/bash\nset -e\n#{cmd}\n")
    FileUtils.chmod(0o755, local_script)

    verbose_flag = verbose? ? {} : { verbose: false }

    # Copy script to Proxmox host
    sh 'scp', '-q', local_script, "#{PROXMOX_HOST}:#{host_script}", **verbose_flag

    # Push script into container
    sh('bash', '-c',
       "ssh #{PROXMOX_HOST} 'pct push #{PROXMOX_VMID} " \
       "#{host_script} #{container_script}' >/dev/null 2>&1", **verbose_flag)

    # Execute script in container
    sh('bash', '-c',
       "ssh #{PROXMOX_HOST} 'pct exec #{PROXMOX_VMID} -- " \
       "bash #{container_script}'", **verbose_flag)
  ensure
    # Cleanup
    FileUtils.rm_f(local_script)
    begin
      sh('bash', '-c',
         "ssh #{PROXMOX_HOST} 'rm -f #{host_script}' >/dev/null 2>&1",
         verbose: false)
    rescue StandardError
      nil
    end
    begin
      sh('bash', '-c',
         "ssh #{PROXMOX_HOST} 'pct exec #{PROXMOX_VMID} -- " \
         "rm -f #{container_script}' >/dev/null 2>&1", verbose: false)
    rescue StandardError
      nil
    end
  end
end

# Default task
task default: %i[format lint test]

##
# Display help information for all available rake tasks
#
# Shows organized list of development, deployment, and diagnostic tasks
# with usage examples.
desc 'Show available tasks'
task :help do
  puts 'Logstash Filterlog Parser'.blue
  puts ''
  puts 'Development:'.green
  puts '  rake install       - Install dependencies'
  puts '  rake test          - Run tests'
  puts '  rake lint          - Run linter'
  puts '  rake format        - Auto-format code'
  puts '  rake dev           - Run format, lint, and test'
  puts ''
  puts 'Deployment:'.green
  puts '  rake deploy        - Deploy to Logstash'
  puts '  rake dry_run       - Show what deploy would do'
  puts '  rake backup        - Backup current configs'
  puts '  rake check         - Validate config'
  puts '  rake restart       - Restart Logstash'
  puts '  rake full_deploy   - Complete deployment pipeline'
  puts ''
  puts 'Diagnostics:'.green
  puts '  rake diagnose      - Check for common issues'
  puts '  rake status        - Show Logstash status'
  puts '  rake logs          - Tail Logstash logs'
  puts '  rake check_port    - Check if port 5140 is in use'
  puts ''
  puts 'Examples:'.yellow
  puts '  DRY_RUN=1 rake deploy                    - Dry-run deployment'
  puts '  REMOTE=1 rake deploy                     - Deploy to default remote host'
  puts '  REMOTE=1 REMOTE_HOST=user@host rake deploy - Deploy to custom remote host'
  puts '  REMOTE=1 rake restart                    - Restart Logstash on remote host'
  puts '  PROXMOX_HOST=root@pve PROXMOX_VMID=100 rake deploy - Deploy via Proxmox pct'
  puts '  rake dev && rake deploy && rake restart'
end

##
# Configure and run RSpec tests
#
# Runs all test files matching the pattern spec/**/*_spec.rb with
# colored documentation output.
RSpec::Core::RakeTask.new(:test) do |t|
  t.pattern = 'spec/**/*_spec.rb'
  t.rspec_opts = ['--format', 'documentation', '--color']
end

##
# Configure and run RuboCop linter
#
# Runs RuboCop with cop names displayed and fails on any violations.
RuboCop::RakeTask.new(:lint) do |t|
  t.options = ['--display-cop-names']
  t.fail_on_error = true
end

##
# Configure and run RuboCop auto-formatter
#
# Runs RuboCop with auto-correct to automatically fix style violations.
# Does not fail on errors to allow fixing of problematic code.
RuboCop::RakeTask.new(:format) do |t|
  t.options = ['--autocorrect-all']
  t.fail_on_error = false
end

##
# Run complete development workflow
#
# Executes format, lint, and test tasks in sequence. Useful for
# pre-deployment validation.
desc 'Run format, lint, and test'
task dev: %i[format lint test] do
  puts '✅ All development checks passed'.green
end

##
# Copy files to deployment destination
#
# Handles file copying for local, remote SSH, and Proxmox deployments.
# For Proxmox, files are first copied to the host, then pushed into
# the container using 'pct push'.
#
# @param source_dir [String] source directory containing files
# @param dest_dir [String] destination directory path
# @param pattern [String] glob pattern for file matching (e.g., '*.rb')
# @return [void]
def copy_files(source_dir, dest_dir, pattern)
  files = Dir.glob(File.join(source_dir, pattern))

  if files.empty?
    puts "  No #{pattern} files to deploy".yellow
    return
  end

  puts "  Copying #{files.length} #{pattern} file(s)...".blue unless verbose?

  files.each do |file|
    puts "    #{File.basename(file)}" if verbose?
    if proxmox_enabled?
      if dry_run?
        puts "[DRY-RUN] pct push #{PROXMOX_VMID} " \
             "#{file} #{dest_dir}/".yellow
      else
        # Copy to Proxmox host temp, then push into container
        temp_file = "/tmp/#{File.basename(file)}"
        sh 'scp', '-q', file, "#{PROXMOX_HOST}:#{temp_file}"
        sh('bash', '-c',
           "ssh #{PROXMOX_HOST} 'pct push #{PROXMOX_VMID} " \
           "#{temp_file} #{dest_dir}/#{File.basename(file)}' >/dev/null 2>&1")
        sh('bash', '-c',
           "ssh #{PROXMOX_HOST} 'rm #{temp_file}' >/dev/null 2>&1")
      end
    elsif remote_enabled?
      if dry_run?
        puts "[DRY-RUN] scp #{file} #{REMOTE_HOST}:#{dest_dir}/".yellow
      else
        sh 'scp', '-q', file, "#{REMOTE_HOST}:#{dest_dir}/"
      end
    else
      run_cmd("sudo cp #{file} #{dest_dir}/")
    end
  end
end

##
# Set ownership of files or directories
#
# Handles ownership setting for local, remote, and Proxmox deployments.
# For Proxmox with glob patterns, uses 'find' command to avoid shell
# expansion issues inside containers.
#
# @param path [String] path to file/directory (may contain glob patterns)
# @param user [String] owner username
# @param group [String] owner group name
# @return [void]
def set_ownership(path, user, group)
  if proxmox_enabled?
    # For Proxmox, use find to handle glob patterns
    if path.include?('*')
      run_cmd("find #{File.dirname(path)} -name " \
              "'#{File.basename(path)}' -exec chown " \
              "#{user}:#{group} {} \\;")
    else
      run_cmd("sudo chown -R #{user}:#{group} #{path}")
    end
  else
    run_cmd("sudo chown -R #{user}:#{group} #{path}")
  end
end

##
# Set permissions on files or directories
#
# Handles permission setting for local, remote, and Proxmox deployments.
# For Proxmox with glob patterns, uses 'find' command to avoid shell
# expansion issues inside containers.
#
# @param path [String] path to file/directory (may contain glob patterns)
# @param mode [String] octal permission mode (e.g., '644')
# @return [void]
def set_permissions(path, mode)
  if proxmox_enabled?
    # For Proxmox, use find to handle glob patterns
    if path.include?('*')
      run_cmd("find #{File.dirname(path)} -name " \
              "'#{File.basename(path)}' -exec chmod " \
              "#{mode} {} \\;")
    else
      run_cmd("sudo chmod #{mode} #{path}")
    end
  else
    run_cmd("sudo chmod #{mode} #{path}")
  end
end

##
# Run diagnostic checks on Logstash configuration
#
# Performs comprehensive checks including:
# - Duplicate input port configurations
# - Port 5140 usage status
# - List of deployed config files
# - Ruby filter syntax validation
desc 'Check for common configuration issues'
task diagnose: %i[check_port check_configs] do
  puts '▶️ Running diagnostics...'.blue
  puts ''

  # Check for duplicate inputs
  puts 'Checking for duplicate input configurations:'.yellow
  run_cmd('grep -r "port.*5140" /etc/logstash/conf.d/ || ' \
          'echo "  ✅ No duplicates found"')
  puts ''

  # Check for syntax errors (locally)
  puts 'Checking Ruby filter syntax (local):'.yellow
  ruby_files = Dir.glob(File.join(RUBY_DIR, '*.rb'))
  if ruby_files.empty?
    puts '  ⚠️️ No Ruby files found'.yellow
  else
    ruby_files.each do |file|
      sh "ruby -c #{file}", verbose: false
      puts "  ✅ #{File.basename(file)}".green
    rescue StandardError
      puts "  ❌ #{File.basename(file)} has syntax errors".red
    end
  end
  puts ''

  puts '✅ Diagnostics complete'.green
end

##
# Check if syslog port 5140 is in use
#
# Uses 'ss' command to check if UDP port 5140 is currently bound.
desc 'Check if port 5140 is in use'
task :check_port do
  puts '▶️ Checking port 5140...'.blue
  run_cmd("sudo ss -lunp | grep :#{SYSLOG_PORT} || " \
          "echo '✅ Port #{SYSLOG_PORT} is available'")
end

##
# Display Logstash service status
#
# Shows systemd service status for the Logstash daemon.
desc 'Show Logstash service status'
task :status do
  run_cmd('sudo systemctl status logstash --no-pager')
end

##
# Tail Logstash service logs in real-time
#
# Monitors journalctl logs for the Logstash service. Press Ctrl+C to stop.
# Handles local, remote SSH, and Proxmox deployments.
desc 'Tail Logstash logs'
task :logs do
  puts 'Monitoring Logstash logs (Ctrl+C to stop)...'.blue
  if proxmox_enabled?
    sh('bash', '-c',
       "ssh #{PROXMOX_HOST} 'pct exec #{PROXMOX_VMID} -- " \
       "journalctl -u logstash -f'")
  elsif remote_enabled?
    sh('bash', '-c',
       "ssh #{REMOTE_HOST} 'sudo journalctl -u logstash -f'")
  else
    run_cmd('sudo journalctl -u logstash -f')
  end
end

##
# Create timestamped backups of current Logstash configuration
#
# Backs up both config files and Ruby filters to timestamped directories.
# Backup format: /etc/logstash/conf.d.backup.YYYYMMDD-HHMMSS
desc 'Backup current Logstash configs'
task :backup do
  puts '▶️ Creating backups...'.blue
  timestamp = Time.now.strftime('%Y%m%d-%H%M%S')
  conf_backup_dir = "#{LOGSTASH_CONF_DIR}.backup.#{timestamp}"
  ruby_backup_dir = "#{LOGSTASH_RUBY_DIR}.backup.#{timestamp}"

  run_cmd("sudo mkdir -p #{conf_backup_dir}")
  run_cmd("sudo mkdir -p #{ruby_backup_dir}")

  # Backup configs - suppress error if nothing to backup
  begin
    run_cmd("sudo cp -r #{LOGSTASH_CONF_DIR}/*.conf #{conf_backup_dir}/ " \
            '2>/dev/null || true')
    # Check if backup dir has files using ls exit code
    run_cmd("sudo ls #{conf_backup_dir}/*.conf >/dev/null 2>&1")
    puts "✅ Config backup created: #{conf_backup_dir}".green
  rescue StandardError
    puts '⚠️️ No configs to backup'.yellow
  end

  # Backup Ruby filters - suppress error if nothing to backup
  begin
    run_cmd("sudo cp -r #{LOGSTASH_RUBY_DIR}/*.rb #{ruby_backup_dir}/ " \
            '2>/dev/null || true')
    # Check if backup dir has files using ls exit code
    run_cmd("sudo ls #{ruby_backup_dir}/*.rb >/dev/null 2>&1")
    puts "✅ Ruby backup created: #{ruby_backup_dir}".green
  rescue StandardError
    puts '⚠️️ No Ruby filters to backup'.yellow
  end
end

##
# Deploy Ruby filter scripts to Logstash
#
# Copies Ruby filter files from ruby/ directory to /etc/logstash/ruby/
# and sets appropriate ownership and permissions.
desc 'Deploy Ruby filters'
task :deploy_ruby do
  puts '▶️ Deploying Ruby filters...'.blue
  run_cmd("sudo rm -rf #{LOGSTASH_RUBY_DIR}/*.rb")
  run_cmd("sudo mkdir -p #{LOGSTASH_RUBY_DIR}")
  copy_files(RUBY_DIR, LOGSTASH_RUBY_DIR, '*.rb')
  set_ownership(LOGSTASH_RUBY_DIR, LOGSTASH_USER, LOGSTASH_GROUP)
  set_permissions("#{LOGSTASH_RUBY_DIR}/*.rb", '644')
end

##
# Deploy Logstash pipeline configuration files
#
# Copies config files from conf/ directory to /etc/logstash/conf.d/
# and sets appropriate ownership and permissions.
desc 'Deploy Logstash configs'
task :deploy_conf do
  return unless Dir.exist?(CONF_DIR)

  puts '▶️ Deploying Logstash configs...'.blue
  run_cmd("sudo rm -rf #{LOGSTASH_CONF_DIR}/*.conf")
  copy_files(CONF_DIR, LOGSTASH_CONF_DIR, '*.conf')
  set_ownership("#{LOGSTASH_CONF_DIR}/*.conf", LOGSTASH_USER, LOGSTASH_GROUP)
  set_permissions("#{LOGSTASH_CONF_DIR}/*.conf", '644')
end

##
# Main deployment task
#
# Executes complete deployment workflow:
# 1. Backup existing configs
# 2. Deploy Ruby filters
# 3. Deploy configuration files
#
# After completion, suggests running diagnose, check, and restart tasks.
desc 'Deploy to Logstash'
task deploy: %i[backup deploy_ruby deploy_conf] do
  puts ''
  puts '✅ Deployment completed'.green
  puts ''
  puts 'Next steps:'.yellow
  puts '  1. rake diagnose  - Check for issues'
  puts '  2. rake check     - Validate configuration'
  puts '  3. rake restart   - Apply changes'
end

##
# Preview deployment without making changes
#
# Runs deployment in dry-run mode by setting DRY_RUN=1 environment variable.
# Shows all commands that would be executed without actually running them.
desc 'Preview deployment without making changes'
task :dry_run do
  puts 'Running deployment in dry-run mode...'.yellow
  ENV['DRY_RUN'] = '1'
  Rake::Task[:deploy].invoke
end

##
# Validate Logstash configuration
#
# Lists deployed config files to verify deployment.
desc 'Validate Logstash configuration'
task :check_configs do
  puts '▶️ Checking deployed configuration files...'.blue

  if proxmox_enabled?
    run_cmd("find #{LOGSTASH_CONF_DIR} -name '*.conf' -exec ls -lh {} \\;")
  else
    run_cmd("ls -lh #{LOGSTASH_CONF_DIR}/*.conf")
  end

  puts ''
  puts '✅ Configuration files deployed'.green
  puts ''
  puts 'Validation will occur on restart'.yellow
end

##
# Restart Logstash service
#
# Performs graceful restart of Logstash:
# 1. Stops the service
# 2. Waits for port release
# 3. Starts the service
# 4. Waits for startup
# 5. Verifies service is active
desc 'Restart Logstash service'
task :restart do
  puts '▶️ Stopping Logstash...'.blue
  run_cmd('sudo systemctl stop logstash')

  # Wait for port to be released
  sleep 2

  puts '▶️ Starting Logstash...'.blue
  run_cmd('sudo systemctl start logstash')

  # Wait for service to start
  sleep 3

  puts '▶️ Checking status...'.blue
  run_cmd('sudo systemctl is-active logstash')

  puts '✅ Logstash restarted'.green
  puts ''
  puts 'Monitor logs:'.yellow
  puts '  rake logs'
end

##
# Complete deployment pipeline
#
# Runs full deployment workflow in sequence:
# 1. Format code
# 2. Lint code
# 3. Run tests
# 4. Backup existing configs
# 5. Deploy files
# 6. Run diagnostics
# 7. Validate configuration
#
# Does not automatically restart - suggests manual restart as final step.
desc 'Complete deployment pipeline ' \
     '(format, lint, test, backup, deploy, check)'
task full_deploy: %i[format lint test backup deploy diagnose check] do
  puts ''
  puts '✅ Full deployment pipeline completed'.green
  puts ''
  puts 'Final step:'.yellow
  puts '  rake restart'
end

##
# Remove temporary files and build artifacts
#
# Cleans up:
# - .bundle directory
# - vendor/bundle directory
# - Vim swap files (.swp, .swo)
# - Backup files (~)
# - macOS .DS_Store files
desc 'Remove temporary files'
task :clean do
  puts '▶️ Cleaning temporary files...'.blue
  FileUtils.rm_rf('.bundle')
  FileUtils.rm_rf('vendor/bundle')

  ['.swp', '.swo', '~', '.DS_Store'].each do |ext|
    Dir.glob("**/*#{ext}").each { |f| FileUtils.rm_f(f) }
  end

  puts '✅ Cleanup completed'.green
end

##
# Install Ruby dependencies
#
# Runs 'bundle install' to install all gems specified in Gemfile.
desc 'Install Ruby dependencies'
task :install do
  puts '▶️ Installing dependencies...'.blue
  sh 'bundle install'
  puts '✅ Dependencies installed'.green
end

##
# Check for duplicate syslog input configurations
#
# Scans deployed config files for multiple instances of port 5140
# configuration, which would cause conflicts. Suggests fixes if
# duplicates are found.
desc 'Fix duplicate input configuration'
task :fix_duplicates do
  puts '▶️ Checking for duplicate input configurations...'.blue

  # For remote/Proxmox, use grep to find duplicates
  if proxmox_enabled? || remote_enabled?
    puts 'Scanning remote config files for port 5140...'.yellow
    begin
      # Use grep to find files with port 5140, count them
      ssh_prefix = if proxmox_enabled?
                     "ssh #{PROXMOX_HOST} 'pct exec #{PROXMOX_VMID} --"
                   else
                     "ssh #{REMOTE_HOST} '"
                   end
      result = `#{ssh_prefix} grep -l "port.*5140" "\
               "#{LOGSTASH_CONF_DIR}/*.conf 2>/dev/null' 2>&1`
      duplicate_files = result.split("\n")

      if duplicate_files.length > 1
        puts "⚠️️ Found #{duplicate_files.length} files with port 5140:".yellow
        duplicate_files.each { |f| puts "  - #{f}" }
        puts ''
        puts 'To fix: Keep only one input file or use different ports'.yellow
      else
        puts '✅ No duplicates found'.green
      end
    rescue StandardError => e
      puts "⚠️️ Could not check for duplicates: #{e.message}".yellow
    end
  else
    # Local check
    duplicate_files = []
    Dir.glob("#{LOGSTASH_CONF_DIR}/*.conf").each do |file|
      duplicate_files << file if File.read(file).match?(/port\s*=>\s*5140/)
    end

    if duplicate_files.length > 1
      puts "⚠️️ Found #{duplicate_files.length} files with port 5140:".yellow
      duplicate_files.each { |f| puts "  - #{f}" }
      puts ''
      puts 'To fix: Keep only one input file or use different ports'.yellow
    else
      puts '✅ No duplicates found'.green
    end
  end
end
