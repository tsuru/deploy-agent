module github.com/tsuru/deploy-agent

go 1.12

require (
	github.com/containerd/containerd v1.5.10
	github.com/containerd/nerdctl v0.11.0
	github.com/docker/cli v20.10.12+incompatible
	github.com/fsouza/go-dockerclient v1.7.4
	github.com/ghodss/yaml v1.0.0
	github.com/kelseyhightower/envconfig v1.3.0
	github.com/mholt/archiver/v3 v3.5.0
	github.com/opencontainers/image-spec v1.0.2
	github.com/opencontainers/runtime-spec v1.0.3-0.20210326190908-1c3f411f0417
	github.com/pkg/errors v0.9.1
	github.com/tsuru/commandmocker v0.0.0-20160909010208-e1d28f4f616a // indirect
	github.com/tsuru/tsuru v0.0.0-20171023121507-c91725578089
	github.com/ulikunitz/xz v0.5.8 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f
)

replace github.com/containerd/stargz-snapshotter/estargz => github.com/containerd/stargz-snapshotter/estargz v0.0.0-20210101143201-d58f43a8235e
