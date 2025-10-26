# frozen_string_literal: true

require 'rspec/core/rake_task'
require 'rubocop/rake_task'

# Default task
task default: %i[format lint test]

desc 'Run RSpec tests'
RSpec::Core::RakeTask.new(:test) do |t|
  t.pattern = 'spec/**/*_spec.rb'
  t.rspec_opts = ['--format', 'documentation', '--color']
end

desc 'Run RuboCop linter'
RuboCop::RakeTask.new(:lint) do |t|
  t.options = ['--display-cop-names']
end

desc 'Auto-format code with RuboCop'
RuboCop::RakeTask.new(:format) do |t|
  t.options = ['--autocorrect-all']
end

desc 'Deploy to Logstash'
task :deploy do
  puts '→ Deploying to Logstash...'

  # Backup existing Ruby filter
  if File.exist?('/etc/logstash/ruby/parse_filterlog.rb')
    puts '  Backing up existing Ruby filter...'
    sh 'sudo cp /etc/logstash/ruby/parse_filterlog.rb /etc/logstash/ruby/parse_filterlog.rb.bak'
  end
  # Deploy Ruby filter
  puts '  Copying Ruby filter...'
  sh 'sudo cp ruby/parse_filterlog.rb /etc/logstash/ruby/'
  sh 'sudo chown logstash:logstash /etc/logstash/ruby/parse_filterlog.rb'
  sh 'sudo chmod 644 /etc/logstash/ruby/parse_filterlog.rb'

  # Deploy config files if they exist
  if Dir.exist?('conf')
    puts '  Copying Logstash configs...'
    Dir.glob('conf/*.conf').each do |conf_file|
      conf_basename = File.basename(conf_file)
      target_conf = "/etc/logstash/conf.d/#{conf_basename}"
      # Backup existing config
      if File.exist?(target_conf)
        puts "    Backing up #{conf_basename}..."
        sh "sudo cp #{target_conf} #{target_conf}.bak"
      end
      sh "sudo cp #{conf_file} /etc/logstash/conf.d/"
    end
    sh 'sudo chown logstash:logstash /etc/logstash/conf.d/*.conf'
    sh 'sudo chmod 644 /etc/logstash/conf.d/*.conf'
  end

  puts '✓ Deployment completed'
  puts ''
  puts 'Next steps:'
  puts '  1. Run: rake check'
  puts '  2. Run: rake restart'
end

desc 'Validate Logstash configuration'
task :check do
  puts '→ Validating Logstash configuration...'
  sh 'sudo -u logstash /usr/share/logstash/bin/logstash --config.test_and_exit -f /etc/logstash/conf.d/'
  puts '✓ Configuration is valid'
end

desc 'Restart Logstash service'
task :restart do
  puts '→ Restarting Logstash...'
  sh 'sudo systemctl restart logstash'
  puts '✓ Logstash restarted'
  puts ''
  puts 'Monitor logs with: sudo journalctl -u logstash -f'
end

desc 'Full deployment pipeline'
task full_deploy: %i[format lint test deploy check] do
  puts ''
  puts '✓ Full deployment pipeline completed'
  puts 'Run: rake restart'
end

desc 'Clean temporary files'
task :clean do
  puts '→ Cleaning temporary files...'
  FileUtils.rm_rf('.bundle')
  FileUtils.rm_rf('vendor/bundle')
  Dir.glob('**/*.swp').each { |f| File.delete(f) }
  Dir.glob('**/*.swo').each { |f| File.delete(f) }
  Dir.glob('**/*~').each { |f| File.delete(f) }
  puts '✓ Cleanup completed'
end

desc 'Show available tasks'
task :help do
  puts 'Available Rake tasks:'
  puts '  rake test          - Run RSpec tests'
  puts '  rake lint          - Run RuboCop linter'
  puts '  rake format        - Auto-format Ruby code'
  puts '  rake deploy        - Deploy to Logstash'
  puts '  rake check         - Validate Logstash config'
  puts '  rake restart       - Restart Logstash service'
  puts '  rake full_deploy   - Full deployment pipeline'
  puts '  rake clean         - Remove temporary files'
end
