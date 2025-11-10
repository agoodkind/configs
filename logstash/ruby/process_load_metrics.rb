# frozen_string_literal: true

##
# Parses system load average metrics from OPNsense system time API response.
#
# This filter extracts and converts load average values from a comma-separated
# string format (e.g. "0.12, 0.15, 0.18") into individual float metrics for
# 1-minute, 5-minute, and 15-minute load averages.
#
# @param event [Object] the Logstash event object containing the 'loadavg' field
#   from the OPNsense system time API response. The event is modified in-place
#   with parsed load average values.
# @return [Array<Object>] array containing the modified event
#
# @see https://docs.opnsense.org/development/api/core/diagnostics.html#get-diagnostics-system-systemtime
#
def filter(event)
  loadavg = event.get('loadavg')

  if loadavg.is_a?(String)
    # Split comma-separated load averages and strip whitespace
    parts = loadavg.split(',').map(&:strip)

    if parts.length == 3
      event.set('[metrics][system][load][1m]', parts[0].to_f)
      event.set('[metrics][system][load][5m]', parts[1].to_f)
      event.set('[metrics][system][load][15m]', parts[2].to_f)
    end
  end

  [event]
end
