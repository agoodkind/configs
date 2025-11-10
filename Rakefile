# frozen_string_literal: true

require_relative 'lib/rake_common'

##
# Parent Rakefile for managing multiple configuration projects
#
# This Rakefile discovers and coordinates tasks across multiple subdirectories
# containing their own Rakefiles. Each child project is namespaced and common
# tasks are exposed at the parent level.
#
# @author Alex Goodkind

# Discover all subdirectories with Rakefiles
CONFIG_PROJECTS = Dir.glob('*/Rakefile').map { |f| File.dirname(f) }.sort.freeze

##
# Execute rake task in specific project directory
#
# @param project [String] project directory name
# @param task_name [String] rake task to execute
# @return [void]
def run_project_task(project, task_name)
  Dir.chdir(project) do
    sh "rake #{task_name}"
  end
end

##
# Check if a project has a specific task
#
# @param project [String] project directory name
# @param task_name [String] task name to check
# @return [Boolean] true if task exists
def project_has_task?(project, task_name)
  Dir.chdir(project) do
    `rake -T #{task_name} 2>/dev/null`.strip.include?(task_name)
  end
end

# Create namespaced tasks for each project
CONFIG_PROJECTS.each do |project|
  namespace_name = project.tr('-', '_').to_sym

  namespace namespace_name do
    desc "List all tasks in #{project}"
    task :tasks do
      puts "Available tasks in #{project}:".blue
      Dir.chdir(project) { sh 'rake -T' }
    end

    desc "Run arbitrary rake task in #{project}"
    task :invoke, [:task_name] do |_t, args|
      task_name = args[:task_name] || 'default'
      puts "▶️  Running #{task_name} in #{project}...".blue
      run_project_task(project, task_name)
    end

    # Define common task shortcuts
    %w[deploy test status restart backup diagnose check clean install].each do |task_name|
      desc "Run #{task_name} in #{project}"
      task task_name.to_sym do
        if project_has_task?(project, task_name)
          puts "▶️  Running #{task_name} in #{project}...".blue
          run_project_task(project, task_name)
        else
          puts "⚠️  Task '#{task_name}' not available in #{project}".yellow
        end
      end
    end
  end
end

##
# Install Ruby dependencies for all projects
desc 'Install dependencies'
task :install do
  puts '▶️  Installing dependencies...'.blue
  sh 'bundle install'
  puts '✅ Dependencies installed'.green
end

# Default task
task default: :help

##
# Display help information
desc 'Show available projects and commands'
task :help do
  puts 'Configuration Projects Manager'.blue
  puts ''
  puts 'Available projects:'.green
  CONFIG_PROJECTS.each do |project|
    puts "  • #{project}"
  end
  puts ''
  puts 'Common commands:'.green
  puts '  rake help                          - Show this help'
  puts '  rake install                       - Install dependencies'
  puts '  rake list                          - List all projects and their tasks'
  puts '  rake deploy_all                    - Deploy all projects'
  puts '  rake test_all                      - Test all projects'
  puts '  rake status_all                    - Show status of all projects'
  puts ''
  puts 'Project-specific commands:'.green
  puts '  rake <project>:deploy              - Deploy specific project'
  puts '  rake <project>:test                - Test specific project'
  puts '  rake <project>:status              - Show status of specific project'
  puts '  rake <project>:tasks               - List all tasks for project'
  puts '  rake <project>:invoke[task_name]   - Run arbitrary task in project'
  puts ''
  puts 'Examples:'.yellow
  CONFIG_PROJECTS.each do |project|
    namespace_name = project.tr('-', '_')
    puts "  rake #{namespace_name}:deploy"
  end
end

##
# List all projects and their available tasks
desc 'List all projects and their tasks'
task :list do
  CONFIG_PROJECTS.each do |project|
    puts ''
    puts "=" * 60
    puts " #{project}".green
    puts "=" * 60
    Dir.chdir(project) { sh 'rake -T' }
  end
  puts ''
end

##
# Deploy all configuration projects
desc 'Deploy all projects'
task :deploy_all do
  puts 'Deploying all configuration projects...'.blue
  puts ''

  results = {}

  CONFIG_PROJECTS.each do |project|
    if project_has_task?(project, 'deploy')
      puts ''
      puts "=" * 60
      puts " Deploying #{project}".green
      puts "=" * 60
      begin
        run_project_task(project, 'deploy')
        results[project] = :success
      rescue StandardError => e
        puts "❌ Failed to deploy #{project}: #{e.message}".red
        results[project] = :failed
      end
    else
      puts "⚠️  Skipping #{project} (no deploy task)".yellow
      results[project] = :skipped
    end
  end

  puts ''
  puts '=' * 60
  puts ' Deployment Summary'.blue
  puts '=' * 60
  results.each do |project, status|
    case status
    when :success
      puts "  ✅ #{project}".green
    when :failed
      puts "  ❌ #{project}".red
    when :skipped
      puts "  ⊘  #{project} (skipped)".yellow
    end
  end
  puts ''

  # Exit with error if any deployments failed
  exit 1 if results.values.include?(:failed)
end

##
# Test all projects that have test tasks
desc 'Test all projects'
task :test_all do
  puts 'Testing all configuration projects...'.blue
  puts ''

  results = {}

  CONFIG_PROJECTS.each do |project|
    if project_has_task?(project, 'test')
      puts ''
      puts "=" * 60
      puts " Testing #{project}".green
      puts "=" * 60
      begin
        run_project_task(project, 'test')
        results[project] = :success
      rescue StandardError => e
        puts "❌ Tests failed in #{project}: #{e.message}".red
        results[project] = :failed
      end
    else
      puts "⚠️  Skipping #{project} (no test task)".yellow
      results[project] = :skipped
    end
  end

  puts ''
  puts '=' * 60
  puts ' Test Summary'.blue
  puts '=' * 60
  results.each do |project, status|
    case status
    when :success
      puts "  ✅ #{project}".green
    when :failed
      puts "  ❌ #{project}".red
    when :skipped
      puts "  ⊘  #{project} (skipped)".yellow
    end
  end
  puts ''

  # Exit with error if any tests failed
  exit 1 if results.values.include?(:failed)
end

##
# Show status of all projects
desc 'Show status of all projects'
task :status_all do
  puts 'Checking status of all configuration projects...'.blue

  CONFIG_PROJECTS.each do |project|
    if project_has_task?(project, 'status')
      puts ''
      puts "=" * 60
      puts " #{project} Status".green
      puts "=" * 60
      begin
        run_project_task(project, 'status')
      rescue StandardError => e
        puts "❌ Failed to get status for #{project}: #{e.message}".red
      end
    else
      puts ''
      puts "⊘  #{project} has no status task".yellow
    end
  end
  puts ''
end

##
# Backup all projects
desc 'Backup all projects'
task :backup_all do
  puts 'Backing up all configuration projects...'.blue

  CONFIG_PROJECTS.each do |project|
    if project_has_task?(project, 'backup')
      puts ''
      puts "=" * 60
      puts " Backing up #{project}".green
      puts "=" * 60
      begin
        run_project_task(project, 'backup')
      rescue StandardError => e
        puts "❌ Failed to backup #{project}: #{e.message}".red
      end
    else
      puts "⊘  #{project} has no backup task".yellow
    end
  end
  puts ''
end

##
# Clean all projects
desc 'Clean all projects'
task :clean_all do
  puts 'Cleaning all configuration projects...'.blue

  CONFIG_PROJECTS.each do |project|
    if project_has_task?(project, 'clean')
      puts ''
      puts "▶️  Cleaning #{project}...".blue
      begin
        run_project_task(project, 'clean')
      rescue StandardError => e
        puts "❌ Failed to clean #{project}: #{e.message}".red
      end
    else
      puts "⊘  #{project} has no clean task".yellow
    end
  end
  puts ''
  puts '✅ Cleanup completed'.green
end

