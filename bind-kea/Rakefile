# frozen_string_literal: true

require 'dotenv/load'
require 'fileutils'

PROXMOX_HOST = ENV.fetch('PROXMOX_HOST')
PROXMOX_VMID = ENV.fetch('PROXMOX_VMID')
ROUTER_USER = ENV.fetch('ROUTER_USER', 'agoodkind')
ROUTER_HOST = ENV.fetch('ROUTER_HOST', 'router.home.goodkind.io')
CONFIG_FILE = 'kea/kea-dhcp-ddns.conf'
DEST_PATH = '/etc/kea/kea-dhcp-ddns.conf'

##
# Check if verbose mode is enabled
#
# @return [Boolean] true if VERBOSE environment variable is set to '1'
def verbose?
  ENV['VERBOSE'] == '1'
end

##
# Execute command on Proxmox host and return output
#
# @param args [Array] command and arguments
# @return [String] command output
def ssh_host(*args)
  IO.popen(['ssh', PROXMOX_HOST] + args, &:read).strip
end

##
# Execute command on Proxmox host with error handling
#
# @param args [Array] command and arguments
# @return [void]
def ssh_host_sh(*)
  sh('ssh', PROXMOX_HOST, *, verbose: verbose?)
end

##
# Execute command on router and return output
#
# @param args [Array] command and arguments
# @return [String] command output
def ssh_router(*args)
  IO.popen(['ssh', "#{ROUTER_USER}@#{ROUTER_HOST}"] + args, &:read).strip
end

##
# Execute command on router with error handling
#
# @param args [Array] command and arguments
# @return [void]
def ssh_router_sh(*)
  sh('ssh', "#{ROUTER_USER}@#{ROUTER_HOST}", *, verbose: verbose?)
end

##
# Execute command in container and return output
#
# @param args [Array] command and arguments
# @return [String] command output
def pct_exec(*)
  ssh_host('pct', 'exec', PROXMOX_VMID, '--', *)
end

##
# Execute command in container with error handling
#
# @param args [Array] command and arguments
# @return [void]
def pct_exec_sh(*)
  ssh_host_sh('pct', 'exec', PROXMOX_VMID, '--', *)
end

##
# Execute pct push from host to container
#
# @param host_path [String] path on Proxmox host
# @param container_path [String] path in container
# @return [void]
def pct_push(host_path, container_path)
  ssh_host_sh('pct', 'push', PROXMOX_VMID, host_path, container_path)
end

##
# Create temp file in /etc/kea (where AppArmor allows access)
#
# @return [String] path to temp file
def create_temp_file
  '/etc/kea/kea-dhcp-ddns.conf.new'
end

##
# Push configuration file to container
#
# @param temp_file [String] destination path in container
# @return [void]
def push_to_container(temp_file)
  host_temp = ssh_host('mktemp')
  sh 'scp', '-q', CONFIG_FILE, "#{PROXMOX_HOST}:#{host_temp}", verbose: verbose?
  pct_push(host_temp, temp_file)
  ssh_host_sh('rm', '-f', host_temp)
  pct_exec_sh('chown', '_kea:_kea', temp_file)
  pct_exec_sh('chmod', '640', temp_file)
end

##
# Test configuration file in container
#
# @param temp_file [String] path to config file in container
# @return [void]
def test_config(temp_file)
  if verbose?
    pct_exec_sh('sudo', '-u', '_kea', '/sbin/kea-dhcp-ddns', '-t', temp_file)
  else
    output = pct_exec('sudo', '-u', '_kea', '/sbin/kea-dhcp-ddns', '-t', temp_file)
    puts "   #{output.split("\n").last}" if output.include?('success')
  end
end

##
# Backup existing config and install new one
#
# @param temp_file [String] path to new config file in container
# @param timestamp [String] timestamp for backup filename
# @return [void]
def backup_and_install(temp_file, timestamp)
  begin
    pct_exec_sh('test', '-f', DEST_PATH)
    pct_exec_sh('cp', DEST_PATH, "#{DEST_PATH}.backup.#{timestamp}")
  rescue StandardError
    # File doesn't exist, skip backup
  end

  pct_exec_sh('mv', temp_file, DEST_PATH)
  pct_exec_sh('chown', '_kea:_kea', DEST_PATH)
  pct_exec_sh('chmod', '640', DEST_PATH)
end

##
# Restart kea-dhcp-ddns service
#
# @return [void]
def restart_kea
  pct_exec_sh('systemctl', 'restart', 'isc-kea-dhcp-ddns-server')
end

##
# Upload config file to router home directory
#
# @param config_file [String] local config file path
# @return [void]
def router_upload(config_file)
  sh 'scp', '-q', config_file, "#{ROUTER_USER}@#{ROUTER_HOST}:", verbose: verbose?
end

##
# Test config file on router
#
# @param binary [String] full path to kea binary
# @param config_file [String] config file name in home directory
# @return [void]
def router_test(binary, config_file)
  ssh_router_sh('sudo', binary, '-t', "$HOME/#{config_file}")
end

##
# Backup and install config on router
#
# @param config_file [String] config file name
# @param dest_path [String] destination path
# @return [void]
def router_install(config_file, dest_path)
  timestamp = Time.now.strftime('%Y%m%d_%H%M%S')

  # Backup existing file if it exists
  begin
    ssh_router_sh('sudo', 'test', '-f', dest_path)
    ssh_router_sh('sudo', 'cp', dest_path, "#{dest_path}.backup.#{timestamp}")
  rescue StandardError
    # File doesn't exist, skip backup
  end

  # Install new file
  ssh_router_sh('sudo', 'mv', "$HOME/#{config_file}", dest_path)
  ssh_router_sh('sudo', 'chown', 'root:wheel', dest_path)
  ssh_router_sh('sudo', 'chmod', '640', dest_path)
end

namespace :router do
  desc 'Push kea-dhcp4.conf to router'
  task :push_dhcp4 do
    puts '▶️  Uploading DHCP4 config...'
    router_upload('kea/kea-dhcp4.conf')
    puts '✅ Config uploaded'
  end

  desc 'Test kea-dhcp4.conf on router'
  task :test_dhcp4 do
    puts '▶️  Testing DHCP4 config...'
    router_test('/usr/local/sbin/kea-dhcp4', 'kea-dhcp4.conf')
    puts '✅ Configuration valid'
  end

  desc 'Install kea-dhcp4.conf on router'
  task :install_dhcp4 do
    puts '▶️  Installing DHCP4 config...'
    router_install('kea-dhcp4.conf', '/usr/local/etc/kea/kea-dhcp4.conf')
    puts '✅ Configuration installed'
  end

  desc 'Deploy kea-dhcp4.conf to router'
  task deploy_dhcp4: %i[push_dhcp4 test_dhcp4 install_dhcp4]

  desc 'Push kea-dhcp6.conf to router'
  task :push_dhcp6 do
    puts '▶️  Uploading DHCP6 config...'
    router_upload('kea/kea-dhcp6.conf')
    puts '✅ Config uploaded'
  end

  desc 'Test kea-dhcp6.conf on router'
  task :test_dhcp6 do
    puts '▶️  Testing DHCP6 config...'
    router_test('/usr/local/sbin/kea-dhcp6', 'kea-dhcp6.conf')
    puts '✅ Configuration valid'
  end

  desc 'Install kea-dhcp6.conf on router'
  task :install_dhcp6 do
    puts '▶️  Installing DHCP6 config...'
    router_install('kea-dhcp6.conf', '/usr/local/etc/kea/kea-dhcp6.conf')
    puts '✅ Configuration installed'
  end

  desc 'Deploy kea-dhcp6.conf to router'
  task deploy_dhcp6: %i[push_dhcp6 test_dhcp6 install_dhcp6]

  desc 'Restart Kea DHCP services on router'
  task :restart do
    puts '▶️  Restarting Kea services...'
    ssh_router_sh('sudo', 'configctl', 'kea', 'restart')
    puts '✅ Services restarted'
  end

  desc 'Deploy all Kea configs to router'
  task deploy: %i[deploy_dhcp4 deploy_dhcp6 restart] do
    puts ''
    puts '✅ Router deployment complete'
  end
end

namespace :bind do
  desc 'List all DNS records'
  task :list_records do
    puts '▶️  Fetching DNS records...'
    puts ''

    # Dump current zone data
    pct_exec_sh('rndc', 'dumpdb', '-all') if verbose?
    sleep 1 # Wait for dump to complete

    # Read the dump file directly
    raw_records = pct_exec('cat', '/var/cache/bind/named_dump.db')

    if raw_records.empty?
      puts '   No records found or dump file does not exist'
    else
      # Parse and filter records in Ruby
      useful_types = %w[A AAAA CNAME TXT NS SOA PTR DHCID]
      records_list = []

      raw_records.split("\n").each do |line|
        # Skip comments, empty lines, and $DATE lines
        next if line.start_with?(';', '$') || line.strip.empty?

        parts = line.split
        next if parts.length < 4

        domain = parts[0]
        ttl = parts[1].to_i
        record_type = parts[3]
        value = parts[4..].join(' ')

        # Filter: only show useful record types, skip RRSIG and root servers
        next unless useful_types.include?(record_type)
        next if line.include?('RRSIG') || line.include?('root-servers')

        records_list << { domain: domain, ttl: ttl, type: record_type, value: value }
      end

      # Print table header
      puts "┌#{'─' * 52}┬#{'─' * 10}┬#{'─' * 8}┬#{'─' * 50}┐"
      printf "│ %-50s │ %-8s │ %-6s │ %-48s │\n", 'Domain', 'TTL', 'Type', 'Value'
      puts "├#{'─' * 52}┼#{'─' * 10}┼#{'─' * 8}┼#{'─' * 50}┤"

      # Print records
      current_domain = nil
      records_list.each do |record|
        # Convert TTL to human readable
        ttl_human = if record[:ttl] >= 86_400
                      "#{record[:ttl] / 86_400}d"
                    elsif record[:ttl] >= 3600
                      "#{record[:ttl] / 3600}h"
                    elsif record[:ttl] >= 60
                      "#{record[:ttl] / 60}m"
                    else
                      "#{record[:ttl]}s"
                    end

        # Add separator between different domains
        puts "├#{'─' * 52}┼#{'─' * 10}┼#{'─' * 8}┼#{'─' * 50}┤" if current_domain && current_domain != record[:domain]

        # Truncate long values
        display_value = record[:value].length > 48 ? "#{record[:value][0..44]}..." : record[:value]

        domain_display = record[:domain] == current_domain ? '' : record[:domain]
        current_domain = record[:domain]

        printf "│ %-50s │ %-8s │ %-6s │ %-48s │\n", domain_display, ttl_human, record[:type], display_value
      end

      # Print table footer
      puts "└#{'─' * 52}┴#{'─' * 10}┴#{'─' * 8}┴#{'─' * 50}┘"
    end

    puts ''
    puts '✅ Records listed'
  end

  desc 'Show BIND status'
  task :status do
    puts '▶️  Checking BIND status...'
    pct_exec_sh('rndc', 'status')
    puts '✅ Status retrieved'
  end
end

namespace :kea do
  desc 'Push kea-dhcp-ddns.conf to container'
  task :push do
    temp_file = create_temp_file
    puts '▶️  Pushing config to container...'
    puts "   Target: #{temp_file}" if verbose?
    push_to_container(temp_file)
    puts '✅ Config pushed'
  end

  desc 'Test kea-dhcp-ddns.conf in container'
  task :test do
    temp_file = create_temp_file
    puts '▶️  Testing configuration...'
    puts "   File: #{temp_file}" if verbose?
    test_config(temp_file)
    puts '✅ Configuration valid'
  end

  desc 'Backup and install kea-dhcp-ddns.conf'
  task :install do
    temp_file = create_temp_file
    timestamp = Time.now.strftime('%Y%m%d_%H%M%S')
    puts '▶️  Backing up and installing...'
    puts "   Backup: #{DEST_PATH}.backup.#{timestamp}" if verbose?
    backup_and_install(temp_file, timestamp)
    puts '✅ Configuration installed'
  end

  desc 'Restart kea-dhcp-ddns service'
  task :restart do
    puts '▶️  Restarting service...'
    restart_kea
    puts '✅ Service restarted'
  end

  desc 'Deploy kea-dhcp-ddns.conf to container'
  task deploy: %i[push test install restart] do
    puts ''
    puts '✅ Deployment complete'
  end
end

desc 'Default task - deploy kea-dhcp-ddns.conf to container'
task default: 'kea:deploy'
