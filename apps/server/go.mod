module github.com/CarlvinceTan/polymux/apps/server

go 1.27.3

replace github.com/CarlvinceTan/polymux/packages/search => ../../packages/search

replace github.com/CarlvinceTan/polymux/packages/vault => ../../packages/vault

replace github.com/CarlvinceTan/polymux/packages/runtime => ../../packages/runtime

replace github.com/CarlvinceTan/polymux/packages/memory => ../../packages/memory

require (
	github.com/CarlvinceTan/polymux/packages/memory v0.0.0
	github.com/CarlvinceTan/polymux/packages/runtime v0.0.0
	github.com/CarlvinceTan/polymux/packages/search v0.0.0
	github.com/CarlvinceTan/polymux/packages/vault v0.0.0
	github.com/MichaelKinsy/PiG v0.5.3
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/clipperhouse/uax29/v2 v2.7.0
	github.com/gorilla/websocket v1.5.3
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/mdp/qrterminal/v3 v3.2.1
	github.com/miekg/dns v1.1.73
	github.com/robfig/cron/v3 v3.0.1
	github.com/tobischo/gokeepasslib/v3 v3.7.1
	golang.org/x/crypto v0.58.0
	golang.org/x/image v0.47.0
	golang.org/x/net v0.61.0
	golang.org/x/text v0.43.0
)

require (
	cloud.google.com/go/compute/metadata v0.10.0 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/MichaelKinsy/PiG/extensions/sdk v0.5.3 // indirect
	github.com/Microsoft/go-winio v0.6.4 // indirect
	github.com/aws/aws-sdk-go-v2 v1.47.4 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.24 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.20.10 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.4 // indirect
	github.com/aws/smithy-go v1.28.6 // indirect
	github.com/charmbracelet/x/ansi v0.11.9 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/gofrs/flock v0.13.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.20.3 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	github.com/tobischo/argon2 v0.2.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/term v0.47.0 // indirect
	modernc.org/libc v1.79.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.61.0 // indirect
	rsc.io/qr v0.2.0 // indirect
)
