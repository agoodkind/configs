# frozen_string_literal: true

##
# Processes and aggregates OPNsense interface statistics metrics.
#
# This filter aggregates network interface metrics by combining multiple entries
# for the same physical interface. OPNsense reports separate statistics for each
# IP address assigned to an interface (IPv4, IPv6, link-layer), but this filter
# combines them into a single metric set per interface name.
#
# Metrics include bytes/packets transmitted and received, errors, drops, collisions,
# MTU, and interface flags. All counters are summed across all addresses on the
# same interface.
#
# @example Interface statistics entry structure
#   {
#     "name": "vtnet0",
#     "received-bytes": 5574919,
#     "sent-bytes": 8694916,
#     "received-packets": 14974,
#     "sent-packets": 28893,
#     ...
#   }
#
# @see https://docs.opnsense.org/development/api/core/diagnostics.html#get-diagnostics-interface-getinterfacestatistics
#

##
# Creates an initial aggregated metrics entry for an interface.
#
# @param iface_data [Hash] the interface data from OPNsense API
# @return [Hash] initialized hash with zero counters and interface metadata
#
def initialize_interface_entry(iface_data)
  {
    'bytes_in' => 0, 'bytes_out' => 0, 'packets_in' => 0, 'packets_out' => 0,
    'errors_in' => 0, 'errors_out' => 0, 'dropped_packets' => 0, 'collisions' => 0,
    'mtu' => iface_data['mtu'], 'flags' => iface_data['flags']
  }
end

##
# Accumulates interface counter values into aggregated metrics.
#
# @param aggregated [Hash] the aggregated metrics hash
# @param iface_name [String] the interface name
# @param iface_data [Hash] the interface data from OPNsense API
# @return [void]
#
def accumulate_interface_counters(aggregated, iface_name, iface_data)
  aggregated[iface_name]['bytes_in'] += (iface_data['received-bytes'] || 0).to_i
  aggregated[iface_name]['bytes_out'] += (iface_data['sent-bytes'] || 0).to_i
  aggregated[iface_name]['packets_in'] += (iface_data['received-packets'] || 0).to_i
  aggregated[iface_name]['packets_out'] += (iface_data['sent-packets'] || 0).to_i
  aggregated[iface_name]['errors_in'] += (iface_data['received-errors'] || 0).to_i
  aggregated[iface_name]['errors_out'] += (iface_data['send-errors'] || 0).to_i
  aggregated[iface_name]['dropped_packets'] += (iface_data['dropped-packets'] || 0).to_i
  aggregated[iface_name]['collisions'] += (iface_data['collisions'] || 0).to_i
end

##
# Aggregates interface statistics by combining IPv4/IPv6/link entries.
#
# @param stats [Hash] the statistics hash from OPNsense API
# @return [Hash] aggregated metrics indexed by interface name
#
def aggregate_interface_stats(stats)
  aggregated = {}

  stats.each_value do |iface_data|
    next unless iface_data.is_a?(Hash) && iface_data['name']

    iface_name = iface_data['name']
    aggregated[iface_name] ||= initialize_interface_entry(iface_data)
    accumulate_interface_counters(aggregated, iface_name, iface_data)
  end

  aggregated
end

##
# Sets aggregated interface metrics into the event.
#
# @param event [Object] the Logstash event object to modify
# @param aggregated [Hash] the aggregated metrics to set
# @return [void]
#
def set_interface_metrics(event, aggregated)
  aggregated.each do |iface_name, metrics|
    event.set("[metrics][interface][#{iface_name}][bytes_in]", metrics['bytes_in'])
    event.set("[metrics][interface][#{iface_name}][bytes_out]", metrics['bytes_out'])
    event.set("[metrics][interface][#{iface_name}][packets_in]", metrics['packets_in'])
    event.set("[metrics][interface][#{iface_name}][packets_out]", metrics['packets_out'])
    event.set("[metrics][interface][#{iface_name}][errors_in]", metrics['errors_in'])
    event.set("[metrics][interface][#{iface_name}][errors_out]", metrics['errors_out'])
    event.set("[metrics][interface][#{iface_name}][dropped_packets]", metrics['dropped_packets'])
    event.set("[metrics][interface][#{iface_name}][collisions]", metrics['collisions'])
    event.set("[metrics][interface][#{iface_name}][mtu]", metrics['mtu'])
    event.set("[metrics][interface][#{iface_name}][flags]", metrics['flags'])
  end
end

##
# Main filter entry point for processing interface statistics.
#
# @param event [Object] the Logstash event object containing the 'statistics' hash
#   from the OPNsense interface statistics API response. The event is modified
#   in-place with aggregated interface metrics.
# @return [Array<Object>] array containing the modified event
#
def filter(event)
  stats = event.get('statistics')

  if stats.is_a?(Hash)
    aggregated = aggregate_interface_stats(stats)
    set_interface_metrics(event, aggregated)
  end

  [event]
end
