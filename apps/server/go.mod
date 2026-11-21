module github.com/CarlvinceTan/polymux/apps/server

go 1.27.4

replace github.com/CarlvinceTan/polymux/packages/search => ../../packages/search

replace github.com/CarlvinceTan/polymux/packages/vault => ../../packages/vault

replace github.com/CarlvinceTan/polymux/packages/runtime => ../../packages/runtime

replace github.com/CarlvinceTan/polymux/packages/memory => ../../packages/memory

require (
	github.com/CarlvinceTan/polymux/packages/memory v0.0.0
	github.com/CarlvinceTan/polymux/packages/runtime v0.0.0
	github.com/CarlvinceTan/polymux/packages/search v0.0.0
	github.com/CarlvinceTan/polymux/packages/vault v0.0.0
	github.com/MichaelKinsy/PiG v0.7.0
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/clipperhouse/uax29/v2 v2.7.0
	github.com/gorilla/websocket v1.5.3
	github.com/mattn/go-sqlite3 v1.14.53
	github.com/mdp/qrterminal/v3 v3.2.1
	github.com/miekg/dns v1.1.73
	github.com/robfig/cron/v3 v3.0.1
	github.com/tobischo/gokeepasslib/v3 v3.7.1
	golang.org/x/crypto v0.58.0
	golang.org/x/image v0.48.0
	golang.org/x/net v0.62.0
	modernc.org/sqlite v1.62.0
)

require (
	cloud.google.com/go/compute/metadata v0.10.0 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/MichaelKinsy/PiG/extensions/sdk v0.7.0 // indirect
	github.com/Microsoft/go-winio v0.6.4 // indirect
	github.com/aws/aws-sdk-go-v2 v1.48.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.27 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.21.0 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.9 // indirect
	github.com/aws/smithy-go v1.28.9 // indirect
	github.com/charmbracelet/x/ansi v0.11.9 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/gofrs/flock v0.13.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.20.4 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	github.com/tobischo/argon2 v0.2.0 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/oauth2 v0.38.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/term v0.48.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	modernc.org/libc v1.81.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	rsc.io/qr v0.2.0 // indirect
)
