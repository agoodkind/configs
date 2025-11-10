# frozen_string_literal: true

##
# Calculates memory usage percentage from OPNsense system resource metrics.
#
# This filter computes the memory utilization percentage based on total and used
# memory values. The percentage is rounded to 2 decimal places for consistent
# metric granularity.
#
# @param event [Object] the Logstash event object containing memory total and used
#   values from the OPNsense system resources API. The event is modified in-place
#   with calculated memory usage percentage.
# @return [Array<Object>] array containing the modified event
#
# @see https://docs.opnsense.org/development/api/core/diagnostics.html#get-diagnostics-system-systemresources
#
def filter(event)
  total = event.get('[memory][total]')
  used = event.get('[memory][used]')

  if total && used && total.to_i.positive?
    percent = (used.to_f / total * 100).round(2)
    event.set('[metrics][system][memory][usage_percent]', percent)
  end

  [event]
end
