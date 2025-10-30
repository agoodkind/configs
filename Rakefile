# frozen_string_literal: true

require 'rspec/core/rake_task'
require 'rubocop/rake_task'
require 'fileutils'

# Configuration
RUBY_DIR = 'ruby'
CONF_DIR = 'conf'
LOGSTASH_RUBY_DIR = '/etc/logstash/ruby'
LOGSTASH_CONF_DIR = '/etc/logstash/conf.d'
LOGSTASH_USER = 'logstash'
LOGSTASH_GROUP = 'logstash'
SYSLOG_PORT = 5140
REMOTE_HOST = 'root@10.250.0.4'

# Colors
class String
  def green = "\e[32m#{self}\e[0m"
  def yellow = "\e[33m#{self}\e[0m"
  def blue = "\e[34m#{self}\e[0m"
  def red = "\e[31m#{self}\e[0m"
end

# Dry-run and remote support
def dry_run?
  ENV['DRY_RUN'] == '1'
end

def remote_enabled?
  ENV['REMOTE'] == '1'
end

def run_cmd(cmd)
  if remote_enabled?
    remote_cmd(cmd)
  elsif dry_run?
    puts "[DRY-RUN] #{cmd}".yellow
  else
    sh cmd
  end
end

def remote_cmd(cmd)
  full_cmd = "ssh #{REMOTE_HOST} '#{cmd}'"
  if dry_run?
    puts "[DRY-RUN] #{full_cmd}".yellow
  else
    sh full_cmd
  end
end

# Default task
task default: %i[format lint test]

# Help
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
  puts '  DRY_RUN=1 rake deploy       - Dry-run deployment'
  puts '  REMOTE=1 rake deploy        - Deploy to root@logs via SSH'
  puts '  REMOTE=1 rake restart       - Restart Logstash on remote host'
  puts '  rake dev && rake deploy && rake restart'
end

# Tests
RSpec::Core::RakeTask.new(:test) do |t|
  t.pattern = 'spec/**/*_spec.rb'
  t.rspec_opts = ['--format', 'documentation', '--color']
end

# Linting
RuboCop::RakeTask.new(:lint) do |t|
  t.options = ['--display-cop-names']
  t.fail_on_error = true
end

# Formatting
RuboCop::RakeTask.new(:format) do |t|
  t.options = ['--autocorrect-all']
  t.fail_on_error = false
end

# Development workflow
desc 'Run format, lint, and test'
task dev: %i[format lint test] do
  puts '✓ All development checks passed'.green
end

# Deployment helpers
def copy_files(source_dir, dest_dir, pattern)
  files = Dir.glob(File.join(source_dir, pattern))

  if files.empty?
    puts "  No #{pattern} files to deploy".yellow
    return
  end

  files.each do |file|
    if remote_enabled?
      puts "  Copying #{file} -> #{REMOTE_HOST}:#{dest_dir}/"
      if dry_run?
        puts "[DRY-RUN] scp #{file} #{REMOTE_HOST}:#{dest_dir}/".yellow
      else
        sh "scp #{file} #{REMOTE_HOST}:#{dest_dir}/"
      end
    else
      puts "  Copying #{file} -> #{dest_dir}/"
      run_cmd("sudo cp #{file} #{dest_dir}/")
    end
  end
end

def set_ownership(path, user, group)
  run_cmd("sudo chown -R #{user}:#{group} #{path}")
end

def set_permissions(path, mode)
  run_cmd("sudo chmod #{mode} #{path}")
end

# Diagnostics
desc 'Check for common configuration issues'
task :diagnose do
  puts '→ Running diagnostics...'.blue
  puts ''

  # Check for duplicate inputs
  puts 'Checking for duplicate input configurations:'.yellow
  run_cmd('grep -r "port.*5140" /etc/logstash/conf.d/ || echo "  ✓ No duplicates found"')
  puts ''

  # Check port usage
  puts 'Checking port 5140 usage:'.yellow
  run_cmd("sudo ss -lunp | grep :5140 || echo '  ✓ Port is free'")
  puts ''

  # List all config files
  puts 'Logstash config files:'.yellow
  run_cmd('ls -lh /etc/logstash/conf.d/*.conf')
  puts ''

  # Check for syntax errors
  puts 'Checking Ruby filter syntax:'.yellow
  run_cmd("test -f #{LOGSTASH_RUBY_DIR}/parse_filterlog.rb && ruby -c #{LOGSTASH_RUBY_DIR}/parse_filterlog.rb || echo '  ⚠ Ruby filter not deployed yet'")
  puts ''

  puts '✓ Diagnostics complete'.green
end

desc 'Check if port 5140 is in use'
task :check_port do
  puts '→ Checking port 5140...'.blue
  run_cmd("sudo ss -lunp | grep :#{SYSLOG_PORT} || echo '✓ Port #{SYSLOG_PORT} is available'")
end

desc 'Show Logstash service status'
task :status do
  run_cmd('sudo systemctl status logstash --no-pager')
end

desc 'Tail Logstash logs'
task :logs do
  puts 'Monitoring Logstash logs (Ctrl+C to stop)...'.blue
  if remote_enabled?
    sh "ssh #{REMOTE_HOST} 'sudo journalctl -u logstash -f'"
  else
    run_cmd('sudo journalctl -u logstash -f')
  end
end

# Backup
desc 'Backup current Logstash configs'
task :backup do
  puts '→ Creating backups...'.blue
  timestamp = Time.now.strftime('%Y%m%d-%H%M%S')
  conf_backup_dir = "#{LOGSTASH_CONF_DIR}.backup.#{timestamp}"
  ruby_backup_dir = "#{LOGSTASH_RUBY_DIR}.backup.#{timestamp}"

  run_cmd("sudo mkdir -p #{conf_backup_dir}")
  run_cmd("sudo mkdir -p #{ruby_backup_dir}")

  if Dir.exist?(LOGSTASH_CONF_DIR)
    run_cmd("sudo cp -r #{LOGSTASH_CONF_DIR}/* #{conf_backup_dir}/")
    puts "✓ Config backup created: #{conf_backup_dir}".green
  else
    puts '⚠ No configs to backup'.yellow
  end

  if Dir.exist?(LOGSTASH_RUBY_DIR)
    run_cmd("sudo cp -r #{LOGSTASH_RUBY_DIR}/* #{ruby_backup_dir}/")
    puts "✓ Ruby backup created: #{ruby_backup_dir}".green
  else
    puts '⚠ No Ruby filters to backup'.yellow
  end
end

# Deploy Ruby filters
desc 'Deploy Ruby filters'
task :deploy_ruby do
  puts '→ Deploying Ruby filters...'.blue
  run_cmd("sudo mkdir -p #{LOGSTASH_RUBY_DIR}")
  copy_files(RUBY_DIR, LOGSTASH_RUBY_DIR, '*.rb')
  set_ownership(LOGSTASH_RUBY_DIR, LOGSTASH_USER, LOGSTASH_GROUP)
  set_permissions("#{LOGSTASH_RUBY_DIR}/*.rb", '644')
end

# Deploy configs
desc 'Deploy Logstash configs'
task :deploy_conf do
  return unless Dir.exist?(CONF_DIR)

  puts '→ Deploying Logstash configs...'.blue
  copy_files(CONF_DIR, LOGSTASH_CONF_DIR, '*.conf')
  set_ownership("#{LOGSTASH_CONF_DIR}/*.conf", LOGSTASH_USER, LOGSTASH_GROUP)
  set_permissions("#{LOGSTASH_CONF_DIR}/*.conf", '644')
end

# Main deploy task
desc 'Deploy to Logstash'
task deploy: %i[backup deploy_ruby deploy_conf] do
  puts ''
  puts '✓ Deployment completed'.green
  puts ''
  puts 'Next steps:'.yellow
  puts '  1. rake diagnose  - Check for issues'
  puts '  2. rake check     - Validate configuration'
  puts '  3. rake restart   - Apply changes'
end

# Dry-run
desc 'Preview deployment without making changes'
task :dry_run do
  puts 'Running deployment in dry-run mode...'.yellow
  ENV['DRY_RUN'] = '1'
  Rake::Task[:deploy].invoke
end

# Validate config
desc 'Validate Logstash configuration'
task :check do
  puts '→ Validating Logstash configuration...'.blue

  begin
    run_cmd('sudo -u logstash /usr/share/logstash/bin/logstash --config.test_and_exit -f /etc/logstash/conf.d/')
    puts '✓ Configuration is valid'.green
  rescue StandardError => e
    puts '✗ Configuration validation failed'.red
    puts ''
    puts 'Run "rake diagnose" to check for common issues'.yellow
    raise e
  end
end

# Restart Logstash
desc 'Restart Logstash service'
task :restart do
  puts '→ Stopping Logstash...'.blue
  run_cmd('sudo systemctl stop logstash')

  # Wait for port to be released
  sleep 2

  puts '→ Starting Logstash...'.blue
  run_cmd('sudo systemctl start logstash')

  # Wait for service to start
  sleep 3

  puts '→ Checking status...'.blue
  run_cmd('sudo systemctl is-active logstash')

  puts '✓ Logstash restarted'.green
  puts ''
  puts 'Monitor logs:'.yellow
  puts '  rake logs'
end

# Full deployment pipeline
desc 'Complete deployment pipeline (format, lint, test, backup, deploy, check)'
task full_deploy: %i[format lint test backup deploy diagnose check] do
  puts ''
  puts '✓ Full deployment pipeline completed'.green
  puts ''
  puts 'Final step:'.yellow
  puts '  rake restart'
end

# Clean temporary files
desc 'Remove temporary files'
task :clean do
  puts '→ Cleaning temporary files...'.blue
  FileUtils.rm_rf('.bundle')
  FileUtils.rm_rf('vendor/bundle')

  ['.swp', '.swo', '~', '.DS_Store'].each do |ext|
    Dir.glob("**/*#{ext}").each { |f| FileUtils.rm_f(f) }
  end

  puts '✓ Cleanup completed'.green
end

# Install dependencies
desc 'Install Ruby dependencies'
task :install do
  puts '→ Installing dependencies...'.blue
  sh 'bundle install'
  puts '✓ Dependencies installed'.green
end

# Fix common issues
desc 'Fix duplicate input configuration'
task :fix_duplicates do
  puts '→ Checking for duplicate input configurations...'.blue

  # Find all files with port 5140
  duplicate_files = []
  Dir.glob("#{LOGSTASH_CONF_DIR}/*.conf").each do |file|
    duplicate_files << file if File.read(file).match?(/port\s*=>\s*5140/)
  end

  if duplicate_files.length > 1
    puts "⚠ Found #{duplicate_files.length} files with port 5140:".yellow
    duplicate_files.each { |f| puts "  - #{f}" }
    puts ''
    puts 'To fix: Keep only one input file or use different ports'.yellow
  else
    puts '✓ No duplicates found'.green
  end
end
