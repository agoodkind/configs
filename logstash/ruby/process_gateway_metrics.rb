# frozen_string_literal: true

##
# Processes OPNsense gateway status metrics from the dpinger monitoring system.
#
# This filter extracts gateway health metrics including latency, packet loss,
# and availability status for each configured gateway. The metrics are indexed
# by gateway name for easy time-series analysis and alerting.
#
# @param event [Object] the Logstash event object containing the 'items' array
#   from the OPNsense gateway status API response. The event is modified in-place
#   with extracted gateway health metrics.
# @return [Array<Object>] array containing the modified event
#
# @see https://docs.opnsense.org/development/api/core/routes.html#get-routes-gateway-status
#
def filter(event)
  items = event.get('items')
  if items.is_a?(Array)
    items.each do |gw|
      next unless gw.is_a?(Hash) && gw['name']

      name = gw['name']

      # Latency metrics in milliseconds
      event.set("[metrics][gateway][#{name}][delay_ms]", gw['delay'])
      event.set("[metrics][gateway][#{name}][stddev_ms]", gw['stddev'])

      # Packet loss percentage
      event.set("[metrics][gateway][#{name}][loss_percent]", gw['loss'])

      # Gateway status (none/down/loss/delay)
      event.set("[metrics][gateway][#{name}][status]", gw['status'])
      event.set("[metrics][gateway][#{name}][status_translated]", gw['status_translated'])

      # Gateway and monitor addresses
      event.set("[metrics][gateway][#{name}][address]", gw['address'])
      event.set("[metrics][gateway][#{name}][monitor]", gw['monitor'])
    end
  end
  [event]
end
