# Shared Rake Utilities

This directory contains common code shared across all configuration project Rakefiles.

## Files

### `rake_common.rb`

Common utilities for deployment tasks including:

#### String Extensions
- `String#green`, `String#yellow`, `String#blue`, `String#red` - Colored terminal output

#### RakeCommon Module

Helper methods available to all Rakefiles:

**Environment Checks:**
- `dry_run?` - Check if DRY_RUN=1 is set
- `verbose?` - Check if VERBOSE=1 is set

**SSH Operations:**
- `ssh_exec(host, *args)` - Execute command on remote host and return output
- `ssh_exec_sh(host, *args)` - Execute command on remote host with error handling

**File Transfer:**
- `scp_to_host(local_path, host, remote_path, quiet: true)` - Copy single file via SCP
- `scp_files_to_host(local_paths, host, remote_path, quiet: true)` - Copy multiple files via SCP
- `rsync_to_host(local_path, host, remote_path, delete: false)` - Sync directory via rsync

**Backup:**
- `create_remote_backup(host, source_path, backup_suffix: nil)` - Create timestamped backup on remote host

**Deployment:**
- `deploy_files_to_host(files, host, dest_dir, pattern, owner: nil, mode: '644')` - Deploy multiple files with ownership/permissions
- `deploy_file_to_host(local_file, host, remote_file, owner: nil, mode: '644')` - Deploy single file with ownership/permissions

## Usage

In each Rakefile:

```ruby
require_relative '../lib/rake_common'

# Include common helpers
include RakeCommon

# Now you can use any RakeCommon methods
def deploy_config
  if dry_run?
    puts "Would deploy files".yellow
    return
  end
  
  deploy_files_to_host(
    ['config1.conf', 'config2.conf'],
    'root@server.example.com',
    '/etc/myapp',
    '*.conf',
    owner: 'myapp:myapp',
    mode: '644'
  )
end
```

## Benefits

- **DRY Principle** - Common code defined once, used everywhere
- **Consistency** - Same deployment behavior across all projects
- **Maintainability** - Update deployment logic in one place
- **Testing** - Easier to test shared functionality

