# deploy-agent v2

Deploy agent helps Tsuru (API) with the tough task of inspecting, building, and pushing Tsuru app images to the container registry.

The current version (v2) does it in a special way which makes Tsuru agnostic of container runtime APIs.
It exposes a well-defined API over a gRPC service that translates all Tsuru operations to Buildkit service - but is not limited to it, e.g. it may be extended to support other build services like [Google Cloud Build][Cloud Build], [kaniko][kaniko], whatever.

[Cloud Build]: https://cloud.google.com/build
[kaniko]: https://github.com/GoogleContainerTools/kaniko

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
