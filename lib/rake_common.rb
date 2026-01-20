# frozen_string_literal: true

##
# Common Rake utilities shared across all configuration projects
#
# This module provides shared functionality for deployment tasks including
# SSH helpers, file operations, and colored terminal output.
#
# @author Alex Goodkind

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
# Common Rake helper methods
module RakeCommon
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
  # Execute command on remote host and return output
  #
  # @param host [String] SSH host (user@hostname)
  # @param args [Array] command and arguments
  # @return [String] command output
  def ssh_exec(host, *args)
    # Use LogLevel=ERROR to suppress "Permanently added..." messages
    IO.popen(['ssh', '-o', 'LogLevel=ERROR', host] + args, &:read).strip
  end

  ##
  # Execute command on remote host with error handling
  #
  # @param host [String] SSH host (user@hostname)
  # @param args [Array] command and arguments
  # @return [void]
  def ssh_exec_sh(host, *)
    sh('ssh', host, *, verbose: verbose?)
  end

  ##
  # Copy file to remote host via SCP
  #
  # @param local_path [String] local file path
  # @param host [String] SSH host (user@hostname)
  # @param remote_path [String] remote destination path
  # @param quiet [Boolean] suppress output (default: true)
  # @return [void]
  def scp_to_host(local_path, host, remote_path, quiet: true)
    args = ['scp']
    args << '-q' if quiet
    args << local_path
    args << "#{host}:#{remote_path}"
    sh(*args, verbose: verbose?)
  end

  ##
  # Copy files to remote host via SCP
  #
  # @param local_paths [Array<String>] local file paths
  # @param host [String] SSH host (user@hostname)
  # @param remote_path [String] remote destination path
  # @param quiet [Boolean] suppress output (default: true)
  # @return [void]
  def scp_files_to_host(local_paths, host, remote_path, quiet: true)
    args = ['scp']
    args << '-q' if quiet
    args.concat(local_paths)
    args << "#{host}:#{remote_path}/"
    sh(*args, verbose: verbose?)
  end

  ##
  # Sync directory to remote host using rsync
  #
  # @param local_path [String] local directory path
  # @param host [String] SSH host (user@hostname)
  # @param remote_path [String] remote destination path
  # @param delete [Boolean] delete files on remote that don't exist locally
  # @return [void]
  def rsync_to_host(local_path, host, remote_path, delete: false)
    args = ['-az']
    args << '--delete' if delete
    args << "#{local_path}/"
    args << "#{host}:#{remote_path}/"

    sh 'rsync', *args, verbose: verbose?
  end

  ##
  # Create timestamped backup on remote host
  #
  # @param host [String] SSH host (user@hostname)
  # @param source_path [String] path to backup
  # @param backup_suffix [String] optional suffix for backup name
  # @return [String] backup path
  def create_remote_backup(host, source_path, backup_suffix: nil)
    timestamp = Time.now.strftime('%Y%m%d_%H%M%S')
    suffix = backup_suffix ? ".#{backup_suffix}" : ''
    backup_path = "#{source_path}.backup#{suffix}.#{timestamp}"

    # Check if source exists and create backup
    ssh_exec_sh(host, 'bash', '-c',
                "if [ -f #{source_path} ]; then cp #{source_path} #{backup_path}; fi")

    backup_path
  end

  ##
  # Deploy files to remote host
  #
  # Copies files via SCP, then sets ownership and permissions
  #
  # @param files [Array<String>] local file paths to deploy
  # @param host [String] SSH host (user@hostname)
  # @param dest_dir [String] destination directory on remote host
  # @param pattern [String] file pattern for cleanup (e.g., '*.conf')
  # @param owner [String] file owner (e.g., 'user:group')
  # @param mode [String] file permissions (e.g., '644')
  # @return [void]
  def deploy_files_to_host(files, host, dest_dir, pattern, owner: nil, mode: '755')
    if dry_run?
      puts "[DRY-RUN] Deploy #{files.length} files to #{host}:#{dest_dir}".yellow
      return
    end

    # Prepare destination - remove and recreate to start fresh
    ssh_exec_sh(host, 'rm', '-rf', dest_dir)
    ssh_exec_sh(host, 'mkdir', '-p', dest_dir)

    # Copy all files
    scp_files_to_host(files, host, dest_dir)

    # Set permissions and ownership
    ssh_exec_sh(host, 'chmod', '755', dest_dir)
    ssh_exec_sh(host, 'chmod', mode, "#{dest_dir}/*")
    ssh_exec_sh(host, 'chown', '-R', owner, dest_dir) if owner
  end

  ##
  # Deploy single file to remote host with ownership and permissions
  #
  # @param local_file [String] local file path
  # @param host [String] SSH host (user@hostname)
  # @param remote_file [String] destination file path on remote host
  # @param owner [String] file owner (e.g., 'user:group')
  # @param mode [String] file permissions (e.g., '640')
  # @return [void]
  def deploy_file_to_host(local_file, host, remote_file, owner: nil, mode: '644')
    if dry_run?
      puts "[DRY-RUN] Deploy #{local_file} to #{host}:#{remote_file}".yellow
      return
    end

    # Copy file
    scp_to_host(local_file, host, remote_file)

    # Set ownership and permissions
    ssh_exec_sh(host, 'chown', owner, remote_file) if owner
    ssh_exec_sh(host, 'chmod', mode, remote_file)
  end
end
