# frozen_string_literal: true

##
# Parses pfSense/OPNsense filterlog entries from firewall packet filter logs.
#
# This filter processes comma-separated filterlog messages and extracts structured
# data from three parsing stages: packet filter base fields, IP header fields
# (IPv4/IPv6), and protocol-specific fields (TCP/UDP/CARP/etc.). The parser handles
# field ordering differences between IPv4 and IPv6, particularly the reversed
# protoname/protonum positions.
#
# @note The three stages of parsing should not be confused with OSI network layers.
#       They represent logical groupings of firewall log fields rather than
#       protocol stack layers.
#
# @note Partial data may be returned depending on forged packet integrity,
#       snap length, and other factors such as hardware corruption of packets.
#
# @example Processing a basic IPv4 TCP log entry
#   event = MockEvent.new('syslog_message' => '222,,,label,wg0,match,pass,in,4,...')
#   filter(event)
#   event.get('action')     # => 'pass'
#   event.get('protoname')  # => 'tcp'
#   event.get('src')        # => '10.250.10.8'
#
# @param event [Object] the Logstash event object containing the syslog_message field
#   to be parsed. The event is modified in-place with extracted fields.
# @return [void] this method modifies the event object directly and returns nothing
#
# @see https://docs.netgate.com/pfsense/en/latest/monitoring/logs/raw-filter-format.html
# @see https://github.com/opnsense/ports/blob/master/opnsense/filterlog/files/description.txt
#
def filter(event)
  # Split the filterlog message into comma-separated fields using a limit of -1
  # to preserve trailing empty fields, which is critical for proper parsing.
  #
  # The -1 parameter ensures that trailing empty fields are included in the array,
  # preventing index misalignment when optional protocol fields are absent.
  #
  # Example format:
  # "222,,,e095...,wg0,match,pass,in,4,0x0,,64,0,0,DF,6,tcp,64,10.250.10.8,10.250.0.4,59532,5601,..."
  fields = event.get('syslog_message').to_s.split(',', -1)

  # ========================================================================
  # STAGE 1: PACKETFILTER BASE FIELDS (Positions 0-8)
  # ========================================================================
  # These fields form the foundation of every filterlog entry and are always
  # present regardless of IP version or protocol type. They describe the firewall
  # rule match and basic packet handling information.
  #
  # Field Structure:
  #   rulenr, subrulenr, anchorname, label | "0", interface, reason, action, dir, ipversion
  #
  # Field Descriptions:
  #   rulenr (0):     Rule number that matched the packet (integer as string)
  #   subrulenr (1):  Sub-rule number for nested rules (may be empty)
  #   anchorname (2): Anchor name for grouped rules (may be empty)
  #   label (3):      Rule label/tracker ID when system returned all labels correctly,
  #                   otherwise "0" for unlabeled or unresolved rules
  #   interface (4):  Network interface name (e.g., wg0, vtnet0, em0)
  #   reason (5):     Match reason (typically "match")
  #   action (6):     Firewall action taken: "pass", "block", "reject", etc.
  #   dir (7):        Packet direction: "in" (inbound) or "out" (outbound)
  #   ipversion (8):  IP protocol version: "4" (IPv4) or "6" (IPv6)
  #
  # Example: "222,,,e0958e2cac30445acb9670fb7311313e,wg0,match,pass,in,4"
  #   rulenr = 222
  #   subrulenr = "" (empty)
  #   anchorname = "" (empty)
  #   label = e0958e2cac30445acb9670fb7311313e
  #   interface = wg0
  #   reason = match
  #   action = pass
  #   dir = in
  #   ipversion = 4
  {
    'rulenr' => 0, 'subrulenr' => 1, 'anchorname' => 2,
    'label' => 3, 'interface' => 4, 'reason' => 5,
    'action' => 6, 'dir' => 7, 'ipversion' => 8
  }.each { |name, idx| event.set(name, fields[idx]) }

  ipversion = fields[8]
  idx = 9 # Starting index for IP header parsing
  protoname = nil

  # ========================================================================
  # STAGE 2: IP HEADER FIELDS (IPv4 or IPv6)
  # ========================================================================
  # The IP header structure differs significantly between IPv4 and IPv6,
  # particularly in field ordering. Most notably, IPv4 places protoname AFTER
  # protonum, while IPv6 places protoname BEFORE protonum.
  #
  # Per the official specification:
  #   IPv4: The protonum/protoname order is reversed compared to IPv6.
  #   IPv6: The protonum/protoname order is reversed compared to IPv4.

  case ipversion
  when '4'
    # IPv4 Header Fields (Positions 9-19)
    #
    # Format: [Packetfilter], ipversion, tos, ecn, ttl, id, offset, flags,
    #         protonum, protoname, length, src, dst
    #
    # Field Descriptions:
    #   tos (9):        Type of Service (hex format, e.g., 0x0, 0xc0)
    #   ecn (10):       Explicit Congestion Notification (may be empty)
    #   ttl (11):       Time To Live (hop count, typically 64, 128, 255)
    #   id (12):        IP identification field for fragmentation
    #   offset (13):    Fragment offset (0 for non-fragmented packets)
    #   ipflags (14):   IP header flags: "DF" (Don't Fragment), "MF" (More Fragments), "none"
    #   protonum (15):  Protocol number (6=TCP, 17=UDP, 1=ICMP, 112=CARP)
    #   protoname (16): Protocol name string (tcp, udp, icmp, carp, etc.)
    #   length (17):    Total IP packet length in bytes
    #   src (18):       Source IPv4 address (dotted decimal notation)
    #   dst (19):       Destination IPv4 address (dotted decimal notation)
    #
    # CRITICAL: protoname comes AFTER protonum at position 16 for IPv4.
    #
    # Example: "4,0x0,,64,0,0,DF,6,tcp,64,10.250.10.8,10.250.0.4"
    #   tos = 0x0
    #   ecn = "" (empty)
    #   ttl = 64
    #   id = 0
    #   offset = 0
    #   ipflags = DF (Don't Fragment)
    #   protonum = 6 (TCP)
    #   protoname = tcp
    #   length = 64
    #   src = 10.250.10.8
    #   dst = 10.250.0.4
    #
    # Protocol-specific data begins at position 20.
    %w[tos ecn ttl id offset ipflags
       protonum protoname length src dst].each_with_index do |name, i|
      event.set(name, fields[idx + i])
    end
    protoname = fields[16] # IPv4: protoname is at absolute position 16
    idx = 20 # Protocol-specific fields start at position 20 for IPv4

  when '6'
    # IPv6 Header Fields (Positions 9-16)
    #
    # Format: [Packetfilter], ipversion, class, flow, hoplimit, protoname,
    #         protonum, length, src, dst
    #
    # Field Descriptions:
    #   class (9):      Traffic Class (QoS field, hex format, e.g., 0x00)
    #   flow (10):      Flow Label (20-bit field for flow identification)
    #   hoplimit (11):  Hop Limit (equivalent to IPv4 TTL, typically 64, 255)
    #   protoname (12): Protocol name string (tcp, udp, icmp6, carp, etc.)
    #   protonum (13):  Protocol number (6=TCP, 17=UDP, 58=ICMPv6, 112=CARP)
    #   length (14):    Payload length in bytes (excluding IPv6 header)
    #   src (15):       Source IPv6 address (colon-hexadecimal notation)
    #   dst (16):       Destination IPv6 address (colon-hexadecimal notation)
    #
    # CRITICAL: protoname comes BEFORE protonum at position 12 for IPv6.
    # This is the key difference from IPv4 field ordering.
    #
    # Example: "6,0x00,12345,64,tcp,6,60,2001:db8:a0b:12f0::1,2001:db8:85a3::8a2e:370:7334"
    #   class = 0x00
    #   flow = 12345
    #   hoplimit = 64
    #   protoname = tcp
    #   protonum = 6 (TCP)
    #   length = 60
    #   src = 2001:db8:a0b:12f0::1
    #   dst = 2001:db8:85a3::8a2e:370:7334
    #
    # Protocol-specific data begins at position 17.
    %w[class flow hoplimit protoname
       protonum length src dst].each_with_index do |name, i|
      event.set(name, fields[idx + i])
    end
    protoname = fields[12] # IPv6: protoname is at absolute position 12
    idx = 17 # Protocol-specific fields start at position 17 for IPv6
  end

  # ========================================================================
  # STAGE 3: PROTOCOL-SPECIFIC FIELDS (TCP, UDP, CARP, etc.)
  # ========================================================================
  # This stage appends protocol-specific fields to the previously parsed base
  # and IP header fields. The starting position varies by IP version:
  # IPv4 starts at position 20, IPv6 starts at position 17.
  #
  # Format notation:
  #   [IPv4 | IPv6] means these fields append to either IPv4 or IPv6 base.

  case protoname
  when 'tcp'
    # TCP Protocol Fields
    #
    # Format: [IPv4 | IPv6], srcport, dstport, datalen, flags, seq, ack, window, urg, options
    #
    # Field Descriptions:
    #   srcport:   Source port number (1-65535)
    #   dstport:   Destination port number (1-65535)
    #   datalen:   TCP data payload length in bytes (0 for handshake packets)
    #   tcpflags:  TCP control flags (S=SYN, A=ACK, F=FIN, R=RST, P=PUSH, U=URG, E=ECE, C=CWR)
    #   seq:       TCP sequence number (32-bit value for byte tracking)
    #   ack:       TCP acknowledgment number (may be empty if ACK flag not set)
    #   window:    TCP window size in bytes (for flow control)
    #   urp:       Urgent pointer (may be empty if URG flag not set)
    #   tcpopts:   TCP options semicolon-separated (mss, nop, wscale, sackOK, TS, eol)
    #
    # IPv4 TCP Example (starting at position 20):
    #   "59532,5601,0,SEC,780946964,,65535,,mss;nop;wscale;nop;nop;TS;sackOK;eol"
    #   srcport = 59532
    #   dstport = 5601
    #   datalen = 0
    #   tcpflags = SEC (SYN+ECE+CWR for TCP handshake with ECN support)
    #   seq = 780946964
    #   ack = "" (empty - no ACK flag set)
    #   window = 65535
    #   urp = "" (empty - no URG flag)
    #   tcpopts = mss;nop;wscale;nop;nop;TS;sackOK;eol
    #
    # IPv6 TCP Example (starting at position 17):
    #   "443,59876,20,SA,987654321,123456789,8192,,mss;nop;wscale"
    #   srcport = 443 (HTTPS)
    #   dstport = 59876
    #   datalen = 20 (bytes of application data)
    #   tcpflags = SA (SYN+ACK for connection establishment response)
    #   seq = 987654321
    #   ack = 123456789
    #   window = 8192
    #   urp = "" (empty)
    #   tcpopts = mss;nop;wscale
    #
    # @note Empty ack and urp fields indicate those TCP features are not active
    #       for this particular packet. tcpopts may also be empty for minimal headers.
    %w[srcport dstport datalen tcpflags
       seq ack window urp tcpopts].each_with_index do |name, i|
      event.set(name, fields[idx + i])
    end

  when 'udp'
    # UDP Protocol Fields
    #
    # Format: [IPv4 | IPv6], srcport, dstport, datalen
    #
    # Field Descriptions:
    #   srcport:  Source port number (1-65535)
    #   dstport:  Destination port number (1-65535, common: 53=DNS, 67/68=DHCP)
    #   datalen:  UDP payload length in bytes
    #
    # UDP is a connectionless protocol with minimal header information compared
    # to TCP. It does not include sequence numbers, acknowledgments, flags, or
    # flow control mechanisms.
    #
    # IPv4 UDP Example (starting at position 20):
    #   "51234,53,80"
    #   srcport = 51234 (ephemeral port)
    #   dstport = 53 (DNS)
    #   datalen = 80 (bytes)
    #
    # IPv6 UDP Example (starting at position 17):
    #   "54321,443,100"
    #   srcport = 54321 (ephemeral port)
    #   dstport = 443 (HTTPS over QUIC/HTTP3)
    #   datalen = 100 (bytes)
    %w[srcport dstport datalen].each_with_index do |name, i|
      event.set(name, fields[idx + i])
    end

  when 'carp'
    # CARP (Common Address Redundancy Protocol) Fields
    #
    # Format: [IPv4 | IPv6], type, ttl | hoplimit, vhid, version, advskew, advbase
    #
    # Field Descriptions:
    #   carp_type:  CARP message type (typically 2 for advertisement)
    #   carp_ttl:   TTL (IPv4) or Hop Limit (IPv6) - typically 255 for local segment
    #   vhid:       Virtual Host ID (1-255) - identifies which CARP group
    #   version:    CARP protocol version (typically 2)
    #   advskew:    Advertisement skew (0-254) - priority offset within group
    #   advbase:    Advertisement base interval (0-255) - seconds between advertisements
    #
    # CARP is used for high availability configurations where multiple firewalls
    # share a virtual IP address. The advskew and advbase values determine which
    # firewall becomes the primary (lowest effective advertisement interval wins).
    #
    # IPv4 CARP Example (starting at position 20):
    #   "2,255,1,2,1,0"
    #   carp_type = 2 (advertisement)
    #   carp_ttl = 255 (local network only)
    #   vhid = 1 (virtual host group 1)
    #   version = 2 (CARP v2)
    #   advskew = 1 (slight priority offset)
    #   advbase = 0 (minimum advertisement interval)
    #
    # IPv6 CARP Example (starting at position 17):
    #   "2,255,5,2,0,1"
    #   carp_type = 2 (advertisement)
    #   carp_ttl = 255 (uses hop limit for IPv6)
    #   vhid = 5 (virtual host group 5)
    #   version = 2 (CARP v2)
    #   advskew = 0 (highest priority - no skew)
    #   advbase = 1 (1 second advertisement interval)
    #
    # @note Lower effective advertisement interval (advbase + advskew/256)
    #       indicates higher priority for becoming the primary CARP node.
    %w[carp_type carp_ttl vhid
       version advskew advbase].each_with_index do |name, i|
      event.set(name, fields[idx + i])
    end
  end

  # ========================================================================
  # IMPLICIT PROTOCOL HANDLING
  # ========================================================================
  # Protocols not explicitly handled above (ICMP, ICMPv6, IGMP, ESP, AH, GRE,
  # and others) do not have additional fields beyond the IP header stage.
  # The parser handles these implicitly by not entering any case branch,
  # leaving only the base and IP header fields populated.
  #
  # Common unhandled protocols:
  #   icmp (1):    Internet Control Message Protocol for IPv4
  #   icmp6 (58):  Internet Control Message Protocol for IPv6
  #   igmp (2):    Internet Group Management Protocol
  #   esp (50):    Encapsulating Security Payload (IPsec)
  #   ah (51):     Authentication Header (IPsec)
  #   gre (47):    Generic Routing Encapsulation (tunneling)
  #   ospf (89):   Open Shortest Path First (routing protocol)
  #   pim (103):   Protocol Independent Multicast
  #
  # @note These protocols will have protoname and protonum set but no
  #       protocol-specific fields like ports or flags.
  #
  # @note Per specification caveats: Partial data may be returned by each
  #       parsing stage depending on forged packet integrity, snap length,
  #       and other factors such as hardware corruption of packets.
end
