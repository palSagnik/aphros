# aphros

A distributed log service with Kubernetes deployment support.

## Socat Jump Server

This project includes a socat-based jump server solution to address kubectl port-forward limitations, specifically the issues mentioned in:

- [Kubernetes issue #72597](https://github.com/kubernetes/kubernetes/issues/72597#issuecomment-693149447) - Limited remote host specification
- [dist-services-with-go issue #1](https://github.com/evdzhurov/dist-services-with-go/issues/1#issuecomment-1171844791) - Connection refused errors

### Quick Start

1. **Deploy the jump server:**

   ```bash
   make jumpserver-deploy
   ```

2. **Start port forwarding:**

   ```bash
   make jumpserver-start
   ```

3. **Test the connection:**

   ```bash
   make jumpserver-test
   ```

4. **Use your service:**
   ```bash
   go run cmd/getservers/main.go -addr localhost:8400
   ```

### How It Works

The socat jump server acts as a intelligent proxy inside your Kubernetes cluster:

- **gRPC Compatible**: Maintains persistent connections to the leader instance
- **Health Checking**: Monitors backend service availability
- **Protocol Bridging**: Handles the complexity of inter-pod communication

### Available Commands

```bash
# Port forwarding management
make jumpserver-start      # Start port forwarding
make jumpserver-stop       # Stop port forwarding
make jumpserver-status     # Check status

# Debugging and monitoring
make jumpserver-logs       # View jump server logs
make jumpserver-test       # Test connectivity

# Deployment
make jumpserver-deploy     # Deploy jump server
make jumpserver-help       # Show all available commands
```

### Default Port Mappings

- **8400**: RPC service (connected to leader pod)
- **8401**: Serf clustering (connected to leader pod)

### Customization

```bash
# Use custom namespace and ports
make jumpserver-start NAMESPACE=production LOCAL_PORT_RPC=9400

# Use different release name and namespace
make jumpserver-deploy RELEASE_NAME=my-aphros NAMESPACE=production

# View help for all configuration options
make jumpserver-help
```

### Troubleshooting

1. **Connection refused errors**: Check if the jump server is deployed and running

   ```bash
   make jumpserver-status
   ```

2. **Port conflicts**: Use custom ports with Makefile variables

   ```bash
   make jumpserver-start LOCAL_PORT_RPC=9400 LOCAL_PORT_SERF=9401
   ```

3. **Service discovery issues**: Use logs to check connectivity

   ```bash
   make jumpserver-logs
   ```

4. **Clean up stuck processes**:
   ```bash
   make jumpserver-cleanup
   ```

The jump server provides detailed logs and status information to help diagnose connectivity issues.
