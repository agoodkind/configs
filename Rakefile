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

# Colors
class String
  def green = "\e[32m#{self}\e[0m"
  def yellow = "\e[33m#{self}\e[0m"
  def blue = "\e[34m#{self}\e[0m"
end

# Dry-run support
def dry_run?
  ENV['DRY_RUN'] == '1'
end

def run_cmd(cmd)
  if dry_run?
    puts "[DRY-RUN] #{cmd}".yellow
  else
    sh cmd
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
  puts '  rake install      - Install dependencies'
  puts '  rake test         - Run tests'
  puts '  rake lint         - Run linter'
  puts '  rake format       - Auto-format code'
  puts '  rake dev          - Run format, lint, and test'
  puts ''
  puts 'Deployment:'.green
  puts '  rake deploy       - Deploy to Logstash'
  puts '  rake dry_run      - Show what deploy would do'
  puts '  rake backup       - Backup current configs'
  puts '  rake check        - Validate config'
  puts '  rake restart      - Restart Logstash'
  puts '  rake full_deploy  - Complete deployment pipeline'
  puts ''
  puts 'Examples:'.yellow
  puts '  DRY_RUN=1 rake deploy  - Dry-run deployment'
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
    puts "  Copying #{file} -> #{dest_dir}/"
    run_cmd("sudo cp #{file} #{dest_dir}/")
  end
end

def set_ownership(path, user, group)
  run_cmd("sudo chown -R #{user}:#{group} #{path}")
end

def set_permissions(path, mode)
  run_cmd("sudo chmod #{mode} #{path}")
end

# Backup
desc 'Backup current Logstash configs'
task :backup do
  puts '→ Creating backups...'.blue
  timestamp = Time.now.strftime('%Y%m%d-%H%M%S')
  backup_dir = "#{LOGSTASH_CONF_DIR}.backup.#{timestamp}"

  run_cmd("sudo mkdir -p #{backup_dir}")
  run_cmd("sudo cp -r #{LOGSTASH_CONF_DIR}/* #{backup_dir}/") if Dir.exist?(LOGSTASH_CONF_DIR)

  puts '✓ Backup created'.green
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
task deploy: %i[deploy_ruby deploy_conf] do
  puts ''
  puts '✓ Deployment completed'.green
  puts ''
  puts 'Next steps:'.yellow
  puts '  1. rake check    - Validate configuration'
  puts '  2. rake restart  - Apply changes'
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
  sh 'sudo -u logstash /usr/share/logstash/bin/logstash --config.test_and_exit -f /etc/logstash/conf.d/'
  puts '✓ Configuration is valid'.green
end

# Restart Logstash
desc 'Restart Logstash service'
task :restart do
  puts '→ Restarting Logstash...'.blue
  run_cmd('sudo systemctl restart logstash')
  puts '✓ Logstash restarted'.green
  puts ''
  puts 'Monitor logs:'.yellow
  puts '  sudo journalctl -u logstash -f'
end

# Full deployment pipeline
desc 'Complete deployment pipeline (format, lint, test, backup, deploy, check)'
task full_deploy: %i[format lint test backup deploy check] do
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
