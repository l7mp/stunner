module github.com/l7mp/stunner

go 1.17

require (
	github.com/pion/dtls/v2 v2.1.5
	github.com/pion/logging v0.2.2
	github.com/pion/transport v0.13.0
	// replace from l7mp/turn
	github.com/pion/turn/v2 v2.0.8
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.1
	sigs.k8s.io/yaml v1.3.0
)

require github.com/fsnotify/fsnotify v1.5.4

require github.com/prometheus/client_golang v1.12.2

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/stun v0.3.5 // indirect
	github.com/pion/udp v0.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.32.1 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	golang.org/x/crypto v0.0.0-20220525230936-793ad666bf5e // indirect
	golang.org/x/sys v0.0.0-20220412211240-33da011f77ad // indirect
	google.golang.org/protobuf v1.26.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c // indirect
)

replace github.com/pion/turn/v2 => github.com/l7mp/turn/v2 v2.0.10
