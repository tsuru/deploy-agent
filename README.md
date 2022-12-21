# deploy-agent v2

Deploy agent helps Tsuru (API) with the tough task of inspecting, building, and pushing Tsuru app images to the container registry.

The current version (v2) does it in a special way which makes Tsuru agnostic of container runtime APIs.
It exposes a well-defined API over a gRPC service that translates all Tsuru operations to Buildkit service - but is not limited to it, e.g. it may be extended to support other build services like [Google Cloud Build][Cloud Build], [kaniko][kaniko], whatever.

[Cloud Build]: https://cloud.google.com/build
[kaniko]: https://github.com/GoogleContainerTools/kaniko
