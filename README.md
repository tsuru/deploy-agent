# deploy-agent v2

Deploy agent helps Tsuru (API) with the tough task of inspecting, building, and pushing Tsuru app images to the container registry.

The current version (v2) does it in a special way which makes Tsuru agnostic of container runtime APIs.
It exposes a well-defined API over a gRPC service that translates all Tsuru operations to Buildkit service - but is not limited to it, e.g. it may be extended to support other build services like [Google Cloud Build][Cloud Build], [kaniko][kaniko], whatever.

[Cloud Build]: https://cloud.google.com/build
[kaniko]: https://github.com/GoogleContainerTools/kaniko

## Remote repository providers

Some container registries (Amazon ECR, Oracle Cloud OCIR) do not create image
repositories on first push — pushes to a nonexistent repository fail (e.g. ECR
returns `404 Not Found`). Since Tsuru names images with one repository per app,
deploy-agent can create the repository before pushing.

Enable it by pointing `--remote-repository-path` (or the
`REMOTE_REPOSITORY_PATH` environment variable) at a JSON file mapping each
registry host to a provider config:

```json
{
  "123456789012.dkr.ecr.us-east-1.amazonaws.com": {
    "provider": "ecr"
  },
  "sa-saopaulo-1.ocir.io": {
    "provider": "oci",
    "compartmentID": "ocid1.compartment.oc1..aaaa...",
    "profile": "DEFAULT",
    "configPath": "/etc/oci/config"
  }
}
```

Registries that auto-create repositories on push (Docker Hub, GCR/Artifact
Registry, Harbor, Distribution) need no entry.

### `ecr` provider

Creates the repository via the AWS API using the default credential chain
(IRSA, instance profile, or environment variables) — the same ambient
credentials used by `docker-credential-ecr-login` for the push itself. The AWS
region is resolved from the optional `"region"` config key, falling back to
the region in the registry hostname, then to the SDK defaults. An
already-existing repository is not an error. The identity needs the
`ecr:CreateRepository` permission on the repository prefix, e.g.:

```json
{
  "Effect": "Allow",
  "Action": ["ecr:CreateRepository"],
  "Resource": "arn:aws:ecr:<region>:<account>:repository/tsuru/*"
}
```

Repositories are created with ECR defaults; to control settings like image
scanning or lifecycle policies, pre-create the repositories with your IaC
tooling instead — the provider treats them as already existing.

## Local Development Setup

To set up your local development environment for deploy-agent, follow these steps:

1. **Install Dependencies**
   - Ensure you have Docker and Docker Compose (or the Docker Compose plugin) installed.
   - Install Go (version 1.24 or higher).
   - Install `protoc` (Protocol Buffers compiler).

2. **Prepare Loopback IP**
   - The project uses a reserved IP (`169.196.255.254`) on the loopback interface for local registry and Docker communication.
   - Use the provided script to set up the loopback IP:

     ```sh
     make setup
     ```

   - This will:
     - Assign the fake IP to your loopback interface.
     - Start Buildkit, Docker Registry, and Docker-in-Docker services using Docker Compose.

3. **Run Tests**
   - To run all tests (including integration tests):

     ```sh
     make test test/integration
     ```

4. **Cleanup**
   - To stop services and remove the fake IP from your loopback interface:

     ```sh
     make cleanup
     ```

5. **Other Useful Commands**
   - Lint the code:

     ```sh
     make lint
     ```

   - Build the container image:

     ```sh
     make build/container-image
     ```

> The `misc/local-dev.sh` script handles loopback IP setup/cleanup and works on both Linux and macOS.
