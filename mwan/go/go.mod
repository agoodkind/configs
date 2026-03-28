module github.com/agoodkind/infra-tools

go 1.26.1

replace github.com/agoodkind/send-email => ../../../send-email

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/agoodkind/send-email v0.0.0-20260328050211-9169e66bce5d
	github.com/mdlayher/vsock v1.2.1
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.11
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require (
	github.com/mdlayher/socket v0.4.1 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)
