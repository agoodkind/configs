def filter(event)
# Split the filterlog message into comma-separated fields
                # Example: "222,,,e095...,wg0,match,pass,in,4,0x0,,64,0,0,DF,6,tcp,64,10.250.10.8,10.250.0.4,59532,5601,..."
                fields = event.get("syslog_message").to_s.split(",", -1)

                # ========================================================================
                # LAYER 1: PACKETFILTER BASE (Fields 0-8)
                # ========================================================================
                # Documentation: rulenr, subrulenr, anchorname, label | "0", interface, reason, action, dir
                # These fields are always present regardless of protocol
                # 
                # Example: "222,,,e0958e2cac30445acb9670fb7311313e,wg0,match,pass,in,4"
                #   Field 0: rulenr = 222
                #   Field 1: subrulenr = "" (empty)
                #   Field 2: anchorname = "" (empty)
                #   Field 3: label = e0958e2cac30445acb9670fb7311313e
                #   Field 4: interface = wg0
                #   Field 5: reason = match
                #   Field 6: action = pass
                #   Field 7: dir = in
                #   Field 8: ipversion = 4
                {
                "rulenr" => 0, "subrulenr" => 1, "anchorname" => 2,
                "label" => 3, "interface" => 4, "reason" => 5,
                "action" => 6, "dir" => 7, "ipversion" => 8
                }.each { |name, idx| event.set(name, fields[idx]) }

                ipversion = fields[8]
                idx = 9  # Starting index for IP layer
                protoname = nil

                # ========================================================================
                # LAYER 2: IP LAYER (IPv4 or IPv6)
                # ========================================================================

                case ipversion
                when "4"
                # IPv4 Format (Fields 9-19):
                # [Packetfilter], ipversion, tos, ecn, ttl, id, offset, flags, protonum, protoname, length, src, dst
                #
                # Example: "4,0x0,,64,0,0,DF,6,tcp,64,10.250.10.8,10.250.0.4"
                #   Field 9: tos = 0x0
                #   Field 10: ecn = "" (empty)
                #   Field 11: ttl = 64
                #   Field 12: id = 0
                #   Field 13: offset = 0
                #   Field 14: ipflags = DF
                #   Field 15: protonum = 6
                #   Field 16: protoname = tcp  <- NOTE: protoname AFTER protonum in IPv4
                #   Field 17: length = 64
                #   Field 18: src = 10.250.10.8
                #   Field 19: dst = 10.250.0.4
                #
                # Next layer starts at field 20
                ["tos", "ecn", "ttl", "id", "offset", "ipflags",
                "protonum", "protoname", "length", "src", "dst"].each_with_index do |name, i|
                event.set(name, fields[idx + i])
                end
                protoname = fields[16]  # IPv4: protoname is at position 16
                idx = 20  # Protocol layer starts at 20 for IPv4

                when "6"
                # IPv6 Format (Fields 9-16):
                # [Packetfilter], ipversion, class, flow, hoplimit, protoname, protonum, length, src, dst
                #
                # Example: "6,0x00,12345,64,tcp,6,60,2001:db8:a0b:12f0::1,2001:db8:85a3::8a2e:370:7334"
                #   Field 9: class = 0x00
                #   Field 10: flow = 12345
                #   Field 11: hoplimit = 64
                #   Field 12: protoname = tcp  <- NOTE: protoname BEFORE protonum in IPv6
                #   Field 13: protonum = 6
                #   Field 14: length = 60
                #   Field 15: src = 2001:db8:a0b:12f0::1
                #   Field 16: dst = 2001:db8:85a3::8a2e:370:7334
                #
                # Next layer starts at field 17
                ["class", "flow", "hoplimit", "protoname",
                "protonum", "length", "src", "dst"].each_with_index do |name, i|
                event.set(name, fields[idx + i])
                end
                protoname = fields[12]  # IPv6: protoname is at position 12
                idx = 17  # Protocol layer starts at 17 for IPv6
                end

                # ========================================================================
                # LAYER 3: PROTOCOL LAYER (TCP, UDP, CARP, etc.)
                # ========================================================================
                # This layer appends to [IPv4 | IPv6] base

                case protoname
                when "tcp"
                # TCP Format: [IPv4 | IPv6], srcport, dstport, datalen, flags, seq, ack, window, urg, options
                #
                # IPv4 TCP Example (starting at field 20):
                #   "59532,5601,0,SEC,780946964,,65535,,mss;nop;wscale;nop;nop;TS;sackOK;eol"
                #   Field 20: srcport = 59532
                #   Field 21: dstport = 5601
                #   Field 22: datalen = 0
                #   Field 23: tcpflags = SEC
                #   Field 24: seq = 780946964
                #   Field 25: ack = "" (empty)
                #   Field 26: window = 65535
                #   Field 27: urp = "" (empty)
                #   Field 28: tcpopts = mss;nop;wscale;nop;nop;TS;sackOK;eol
                #
                # IPv6 TCP Example (starting at field 17):
                #   "443,59876,20,SA,987654321,123456789,8192,,mss;nop;wscale"
                #   Field 17: srcport = 443
                #   Field 18: dstport = 59876
                #   Field 19: datalen = 20
                #   Field 20: tcpflags = SA
                #   Field 21: seq = 987654321
                #   Field 22: ack = 123456789
                #   Field 23: window = 8192
                #   Field 24: urp = "" (empty)
                #   Field 25: tcpopts = mss;nop;wscale
                ["srcport", "dstport", "datalen", "tcpflags",
                "seq", "ack", "window", "urp", "tcpopts"].each_with_index do |name, i|
                event.set(name, fields[idx + i])
                end

                when "udp"
                # UDP Format: [IPv4 | IPv6], srcport, dstport, datalen
                #
                # IPv4 UDP Example (starting at field 20):
                #   "51234,53,80"
                #   Field 20: srcport = 51234
                #   Field 21: dstport = 53
                #   Field 22: datalen = 80
                #
                # IPv6 UDP Example (starting at field 17):
                #   "54321,443,100"
                #   Field 17: srcport = 54321
                #   Field 18: dstport = 443
                #   Field 19: datalen = 100
                ["srcport", "dstport", "datalen"].each_with_index do |name, i|
                event.set(name, fields[idx + i])
                end

                when "carp"
                # CARP Format: [IPv4 | IPv6], type, ttl | hoplimit, vhid, version, advskew, advbase
                #
                # IPv4 CARP Example (starting at field 20):
                #   "2,255,1,2,1,0"
                #   Field 20: carp_type = 2
                #   Field 21: carp_ttl = 255
                #   Field 22: vhid = 1
                #   Field 23: version = 2
                #   Field 24: advskew = 1
                #   Field 25: advbase = 0
                #
                # IPv6 CARP Example (starting at field 17):
                #   "2,255,5,2,0,1"
                #   Field 17: carp_type = 2
                #   Field 18: carp_ttl = 255  (uses hoplimit for IPv6)
                #   Field 19: vhid = 5
                #   Field 20: version = 2
                #   Field 21: advskew = 0
                #   Field 22: advbase = 1
                ["carp_type", "carp_ttl", "vhid",
                "version", "advskew", "advbase"].each_with_index do |name, i|
                event.set(name, fields[idx + i])
                end
                end

                # Note: ICMP and other protocols that do not have additional fields
                # will simply not append any protocol-specific fields
end