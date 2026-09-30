# frozen_string_literal: true

require 'digest'
require 'fileutils'
require 'ipaddr'
require 'json'
require 'securerandom'
require 'shellwords'
require 'time'
require 'uri'
require 'yaml'

module MwanAcceptance
  CURL_JSON_FORMAT = ['%', '{json}'].join.freeze

  class InvalidPlan < StandardError; end
  class Failure < StandardError; end
  class Interrupted < StandardError; end
end

require_relative 'mwan_acceptance/plan'
require_relative 'mwan_acceptance/processes'
require_relative 'mwan_acceptance/preflight'
require_relative 'mwan_acceptance/packets'
require_relative 'mwan_acceptance/history'
require_relative 'mwan_acceptance/engine'
