# frozen_string_literal: true

require_relative '../ruby/parse_filterlog'

##
# Mock Logstash event class for testing purposes.
#
# This class simulates the behavior of a Logstash event object,
# providing methods to get and set event data fields.
#
# @example Creating and manipulating a mock event
#   event = MockEvent.new('field' => 'value')
#   event.get('field')  # => 'value'
#   event.set('new_field', 'new_value')
#
class MockEvent
  # @return [Hash] the internal data storage for the event
  attr_reader :data

  ##
  # Initializes a new mock event with optional data.
  #
  # @param data [Hash] initial event data
  #
  def initialize(data = {})
    @data = data
  end

  ##
  # Retrieves the value of a field from the event data.
  #
  # @param key [String] the field name to retrieve
  # @return [Object, nil] the value associated with the key, or nil if not found
  #
  def get(key)
    @data[key]
  end

  ##
  # Sets the value of a field in the event data.
  #
  # @param key [String] the field name to set
  # @param value [Object] the value to associate with the key
  # @return [Object] the value that was set
  #
  def set(key, value)
    @data[key] = value
  end

  ##
  # Converts the event data to a hash.
  #
  # @return [Hash] the event data as a hash
  #
  def to_hash
    @data
  end
end

##
# Loads sample firewall log entries from a test data file.
#
# Reads the sample_logs.txt file located in the test_data directory
# relative to the current file's location. Returns an empty array if
# the file does not exist.
#
# @return [Array<String>] array of log lines with whitespace stripped
#   and empty lines removed
#
def load_sample_logs
  file_path = File.join(__dir__, '..', 'test_data', 'sample_logs.txt')
  return [] unless File.exist?(file_path)

  File.readlines(file_path).map(&:strip).reject(&:empty?)
end

RSpec.describe 'parse_filterlog' do
  # ========================================================================
  # REAL LOG DATA TESTS - Process entire log file
  # ========================================================================

  describe 'real log data from sample_logs.txt' do
    let(:sample_logs) { load_sample_logs }

    before(:all) do
      @parse_errors = []
      @parse_stats = {
        total: 0,
        ipv4: 0,
        ipv6: 0,
        tcp: 0,
        udp: 0,
        icmp: 0,
        other_proto: 0,
        pass: 0,
        block: 0
      }
    end

    it 'loads sample logs successfully' do
      expect(sample_logs).not_to be_empty
      puts "\n✓ Loaded #{sample_logs.count} log entries from test_data/sample_logs.txt"
    end

    ##
    # Tests parsing of all sample firewall logs without errors.
    #
    # This test processes each log entry from the sample file and collects
    # statistics about protocol types, IP versions, and actions. Any parsing
    # errors are captured and reported.
    #
    it 'parses all sample logs without errors' do
      skip 'No sample logs found' if sample_logs.empty?

      sample_logs.each_with_index do |log_line, index|
        event = MockEvent.new('syslog_message' => log_line)
        filter(event)

        # Collect statistics
        @parse_stats[:total] += 1
        @parse_stats[:ipv4] += 1 if event.get('ipversion') == '4'
        @parse_stats[:ipv6] += 1 if event.get('ipversion') == '6'
        @parse_stats[:tcp] += 1 if event.get('protoname') == 'tcp'
        @parse_stats[:udp] += 1 if event.get('protoname') == 'udp'
        @parse_stats[:icmp] += 1 if event.get('protoname')&.include?('icmp')
        @parse_stats[:other_proto] += 1 unless %w[tcp udp].include?(event.get('protoname'))
        @parse_stats[:pass] += 1 if event.get('action') == 'pass'
        @parse_stats[:block] += 1 if event.get('action') == 'block'
      rescue StandardError => e
        @parse_errors << { line: index + 1, log: log_line[0..80], error: e.message }
      end

      # Report errors if any
      unless @parse_errors.empty?
        puts "\n❌ Parse Errors:"
        @parse_errors.first(5).each do |err|
          puts "  Line #{err[:line]}: #{err[:error]}"
          puts "    Log: #{err[:log]}..."
        end
        puts "  (showing first 5 of #{@parse_errors.count} errors)" if @parse_errors.count > 5
      end

      expect(@parse_errors).to be_empty, "Failed to parse #{@parse_errors.count} log entries"
    end

    it 'prints parsing statistics' do
      skip 'No sample logs found' if sample_logs.empty?

      puts "\n📊 Parsing Statistics:"
      puts "  Total logs processed: #{@parse_stats[:total]}"
      puts "  IPv4: #{@parse_stats[:ipv4]}"
      puts "  IPv6: #{@parse_stats[:ipv6]}"
      puts "  TCP: #{@parse_stats[:tcp]}"
      puts "  UDP: #{@parse_stats[:udp]}"
      puts "  ICMP: #{@parse_stats[:icmp]}"
      puts "  Other protocols: #{@parse_stats[:other_proto]}"
      puts "  Pass: #{@parse_stats[:pass]}"
      puts "  Block: #{@parse_stats[:block]}"
    end

    ##
    # Tests extraction of basic fields from IPv4 TCP log entries.
    #
    # Verifies that IP version, protocol name, source/destination addresses,
    # and port numbers are correctly parsed from TCP traffic logs.
    #
    it 'extracts basic fields from first IPv4 TCP log' do
      skip 'No sample logs found' if sample_logs.empty?

      tcp_log = sample_logs.find { |log| log.include?(',tcp,') && log.split(',')[8] == '4' }
      skip 'No IPv4 TCP log found' unless tcp_log

      event = MockEvent.new('syslog_message' => tcp_log)
      filter(event)

      expect(event.get('ipversion')).to eq('4')
      expect(event.get('protoname')).to eq('tcp')
      expect(event.get('src')).not_to be_nil
      expect(event.get('dst')).not_to be_nil
      expect(event.get('srcport')).not_to be_nil
      expect(event.get('dstport')).not_to be_nil
    end

    ##
    # Tests extraction of basic fields from IPv6 log entries.
    #
    # Verifies that IPv6 addresses are correctly parsed and formatted
    # according to IPv6 address notation standards.
    #
    it 'extracts basic fields from first IPv6 log' do
      skip 'No sample logs found' if sample_logs.empty?

      ipv6_log = sample_logs.find { |log| log.split(',')[8] == '6' }
      skip 'No IPv6 log found' unless ipv6_log

      event = MockEvent.new('syslog_message' => ipv6_log)
      filter(event)

      expect(event.get('ipversion')).to eq('6')
      expect(event.get('protoname')).not_to be_nil
      expect(event.get('src')).to match(/[0-9a-f:]/) # IPv6 format
      expect(event.get('dst')).to match(/[0-9a-f:]/)
    end

    ##
    # Tests protocol detection across all sample logs.
    #
    # Identifies all unique protocols present in the sample log file
    # and displays them for verification.
    #
    it 'correctly identifies all protocols' do
      skip 'No sample logs found' if sample_logs.empty?

      protocols = sample_logs.map do |log|
        event = MockEvent.new('syslog_message' => log)
        filter(event)
        event.get('protoname')
      end.uniq.compact.sort

      expect(protocols).not_to be_empty
      puts "\n✓ Found protocols: #{protocols.join(', ')}"
    end

    ##
    # Tests interface detection across all sample logs.
    #
    # Identifies all unique network interfaces present in the sample
    # log file and displays them for verification.
    #
    it 'correctly identifies all interfaces' do
      skip 'No sample logs found' if sample_logs.empty?

      interfaces = sample_logs.map do |log|
        event = MockEvent.new('syslog_message' => log)
        filter(event)
        event.get('interface')
      end.uniq.compact.sort

      expect(interfaces).not_to be_empty
      puts "\n✓ Found interfaces: #{interfaces.join(', ')}"
    end

    ##
    # Validates presence of required fields in all log entries.
    #
    # Ensures that essential firewall log fields are present and non-empty
    # in every parsed log entry. Reports any missing fields with context.
    #
    it 'validates all entries have required fields' do
      skip 'No sample logs found' if sample_logs.empty?

      missing_fields = []

      sample_logs.each_with_index do |log, index|
        event = MockEvent.new('syslog_message' => log)
        filter(event)

        required_fields = %w[rulenr interface action dir ipversion src dst]
        required_fields.each do |field|
          missing_fields << { line: index + 1, field: field, log: log[0..60] } if event.get(field).nil? || event.get(field).to_s.empty?
        end
      end

      if missing_fields.any?
        puts "\n⚠️  Missing required fields (first 5):"
        missing_fields.first(5).each do |mf|
          puts "  Line #{mf[:line]}, missing '#{mf[:field]}': #{mf[:log]}..."
        end
      end

      expect(missing_fields).to be_empty
    end
  end

  # ========================================================================
  # HARDCODED TEST CASES - Specific scenarios
  # ========================================================================

  describe 'IPv4 TCP parsing - hardcoded tests' do
    ##
    # Tests complete parsing of an IPv4 TCP packet with all fields.
    #
    # Validates extraction of all three layers of firewall log data:
    # - Layer 1: Packetfilter base fields (rule number, interface, action)
    # - Layer 2: IPv4 header fields (TOS, TTL, flags)
    # - Layer 3: TCP protocol fields (ports, flags, sequence numbers)
    #
    it 'correctly parses IPv4 TCP packet with all fields' do
      message = '222,,,e0958e2cac30445acb9670fb7311313e,wg0,match,pass,in,4,0x0,,64,0,0,DF,6,tcp,64,10.250.10.8,10.250.0.4,59532,5601,0,SEC,780946964,,65535,,mss;nop;wscale'

      event = MockEvent.new('syslog_message' => message)
      filter(event)

      # Layer 1: Packetfilter base
      expect(event.get('rulenr')).to eq('222')
      expect(event.get('label')).to eq('e0958e2cac30445acb9670fb7311313e')
      expect(event.get('interface')).to eq('wg0')
      expect(event.get('action')).to eq('pass')
      expect(event.get('dir')).to eq('in')
      expect(event.get('ipversion')).to eq('4')

      # Layer 2: IPv4
      expect(event.get('tos')).to eq('0x0')
      expect(event.get('ttl')).to eq('64')
      expect(event.get('ipflags')).to eq('DF')
      expect(event.get('protoname')).to eq('tcp')
      expect(event.get('src')).to eq('10.250.10.8')
      expect(event.get('dst')).to eq('10.250.0.4')

      # Layer 3: TCP
      expect(event.get('srcport')).to eq('59532')
      expect(event.get('dstport')).to eq('5601')
      expect(event.get('datalen')).to eq('0')
      expect(event.get('tcpflags')).to eq('SEC')
      expect(event.get('seq')).to eq('780946964')
      expect(event.get('window')).to eq('65535')
      expect(event.get('tcpopts')).to eq('mss;nop;wscale')
    end

    ##
    # Tests handling of empty TCP optional fields.
    #
    # Verifies that the parser correctly handles TCP packets where
    # optional fields like ACK number, urgent pointer, and TCP options
    # are empty or not present.
    #
    it 'handles empty TCP fields correctly' do
      message = '100,,,label123,vtnet0,match,block,out,4,0x0,,64,12345,0,none,6,tcp,60,192.168.1.1,8.8.8.8,443,80,20,S,123456789,,1024,,'

      event = MockEvent.new('syslog_message' => message)
      filter(event)

      expect(event.get('ack')).to eq('')
      expect(event.get('urp')).to eq('')
      expect(event.get('tcpopts')).to eq('')
    end
  end

  describe 'IPv4 UDP parsing - hardcoded tests' do
    ##
    # Tests parsing of an IPv4 UDP packet.
    #
    # Validates extraction of UDP-specific fields including source and
    # destination ports and data length. Also verifies that TCP-specific
    # fields are not incorrectly populated.
    #
    it 'correctly parses IPv4 UDP packet' do
      message = '150,2,,dns-label,vtnet1,match,pass,in,4,0x0,,64,54321,0,none,17,udp,100,192.168.1.100,8.8.8.8,51234,53,80'

      event = MockEvent.new('syslog_message' => message)
      filter(event)

      expect(event.get('rulenr')).to eq('150')
      expect(event.get('subrulenr')).to eq('2')
      expect(event.get('interface')).to eq('vtnet1')
      expect(event.get('action')).to eq('pass')
      expect(event.get('protoname')).to eq('udp')
      expect(event.get('src')).to eq('192.168.1.100')
      expect(event.get('dst')).to eq('8.8.8.8')
      expect(event.get('srcport')).to eq('51234')
      expect(event.get('dstport')).to eq('53')
      expect(event.get('datalen')).to eq('80')

      # Should not have TCP fields
      expect(event.get('tcpflags')).to be_nil
      expect(event.get('seq')).to be_nil
    end
  end

  describe 'IPv6 TCP parsing - hardcoded tests' do
    ##
    # Tests parsing of an IPv6 TCP packet.
    #
    # Validates extraction of IPv6-specific fields including traffic class,
    # flow label, and hop limit, along with standard TCP fields like flags,
    # sequence numbers, and acknowledgment numbers.
    #
    it 'correctly parses IPv6 TCP packet' do
      message = '200,,,ipv6-label,wg1,match,pass,in,6,0x00,12345,64,tcp,6,60,2001:db8:a0b:12f0::1,2001:db8:85a3::8a2e:370:7334,443,59876,20,SA,987654321,123456789,8192,,mss;nop;wscale'

      event = MockEvent.new('syslog_message' => message)
      filter(event)

      expect(event.get('rulenr')).to eq('200')
      expect(event.get('interface')).to eq('wg1')
      expect(event.get('ipversion')).to eq('6')
      expect(event.get('class')).to eq('0x00')
      expect(event.get('flow')).to eq('12345')
      expect(event.get('hoplimit')).to eq('64')
      expect(event.get('protoname')).to eq('tcp')
      expect(event.get('protonum')).to eq('6')
      expect(event.get('src')).to eq('2001:db8:a0b:12f0::1')
      expect(event.get('dst')).to eq('2001:db8:85a3::8a2e:370:7334')
      expect(event.get('srcport')).to eq('443')
      expect(event.get('dstport')).to eq('59876')
      expect(event.get('tcpflags')).to eq('SA')
      expect(event.get('seq')).to eq('987654321')
      expect(event.get('ack')).to eq('123456789')
    end
  end

  describe 'IPv6 UDP parsing - hardcoded tests' do
    ##
    # Tests parsing of an IPv6 UDP packet.
    #
    # Validates extraction of IPv6 addresses in full colon notation
    # along with UDP port and data length information.
    #
    it 'correctly parses IPv6 UDP packet' do
      message = '175,,,udp6-label,vtnet0,match,pass,out,6,0x00,0,64,udp,17,120,fd00::1,fd00::2,54321,443,100'

      event = MockEvent.new('syslog_message' => message)
      filter(event)

      expect(event.get('ipversion')).to eq('6')
      expect(event.get('protoname')).to eq('udp')
      expect(event.get('src')).to eq('fd00::1')
      expect(event.get('dst')).to eq('fd00::2')
      expect(event.get('srcport')).to eq('54321')
      expect(event.get('dstport')).to eq('443')
      expect(event.get('datalen')).to eq('100')
    end
  end

  describe 'CARP protocol parsing - hardcoded tests' do
    ##
    # Tests parsing of CARP (Common Address Redundancy Protocol) packets.
    #
    # Validates extraction of CARP-specific fields including virtual host ID,
    # advertisement skew, and base values used for high availability configurations.
    #
    it 'correctly parses IPv4 CARP packet' do
      message = '300,,,carp-label,vtnet0,match,pass,in,4,0xc0,,255,0,0,none,112,carp,36,192.168.1.1,224.0.0.18,2,255,1,2,1,0'

      event = MockEvent.new('syslog_message' => message)
      filter(event)

      expect(event.get('protoname')).to eq('carp')
      expect(event.get('src')).to eq('192.168.1.1')
      expect(event.get('dst')).to eq('224.0.0.18')
      expect(event.get('carp_type')).to eq('2')
      expect(event.get('carp_ttl')).to eq('255')
      expect(event.get('vhid')).to eq('1')
      expect(event.get('version')).to eq('2')
      expect(event.get('advskew')).to eq('1')
      expect(event.get('advbase')).to eq('0')
    end
  end

  describe 'edge cases - hardcoded tests' do
    ##
    # Tests handling of minimal firewall log entries.
    #
    # Verifies that the parser correctly handles ICMP packets and
    # block actions that do not include port information.
    #
    it 'handles minimal firewall log (block without port info)' do
      message = '10,,,simple,em0,match,block,in,4,0x0,,64,0,0,none,1,icmp,60,1.2.3.4,5.6.7.8'

      event = MockEvent.new('syslog_message' => message)
      filter(event)

      expect(event.get('rulenr')).to eq('10')
      expect(event.get('action')).to eq('block')
      expect(event.get('protoname')).to eq('icmp')
      expect(event.get('srcport')).to be_nil
    end

    ##
    # Tests handling of empty anchorname and subrulenr fields.
    #
    # Verifies that the parser correctly processes log entries where
    # optional fields like anchor name and sub-rule number are empty strings.
    #
    it 'handles empty anchorname and subrulenr' do
      message = '5,,,test,wg0,match,pass,in,4,0x0,,64,0,0,none,6,tcp,40,10.0.0.1,10.0.0.2,80,443,0,S,1234,5678,1024,,'

      event = MockEvent.new('syslog_message' => message)
      filter(event)

      expect(event.get('subrulenr')).to eq('')
      expect(event.get('anchorname')).to eq('')
      expect(event.get('action')).to eq('pass')
    end
  end
end
