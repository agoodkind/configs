#!/usr/bin/env ruby
# frozen_string_literal: true

require 'optparse'
require_relative '../lib/mwan_acceptance'

options = {}
parser = OptionParser.new do |arguments|
  arguments.banner = 'Usage: mwan_acceptance.rb --plan PLAN.json --output NEW_DIRECTORY'
  arguments.on('--plan PATH') { |path| options[:plan] = path }
  arguments.on('--output PATH') { |path| options[:output] = path }
end

begin
  parser.parse!
  raise MwanAcceptance::InvalidPlan, parser.to_s unless ARGV.empty? && options.keys.sort == %i[output plan]

  plan = MwanAcceptance::Plan.new(options.fetch(:plan))
  Dir.mkdir(options.fetch(:output), 0o700)
rescue MwanAcceptance::InvalidPlan, OptionParser::ParseError, SystemCallError => e
  warn e.message
  exit 64
end

exit MwanAcceptance::Engine.new(plan, File.expand_path(options.fetch(:output))).run
