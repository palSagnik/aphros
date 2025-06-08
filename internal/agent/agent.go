package agent

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	api "github.com/palSagnik/aphros/api/v1"
	"github.com/palSagnik/aphros/internal/auth"
	"github.com/palSagnik/aphros/internal/discovery"
	"github.com/palSagnik/aphros/internal/log"
	"github.com/palSagnik/aphros/internal/server"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Agent represents a distributed log agent that coordinates replication,
// service discovery, and gRPC server functionality. It manages the lifecycle
// of log operations, membership in a distributed cluster, and handles
// graceful shutdown procedures. The Agent embeds a Config for configuration
// settings and maintains references to core components including the log,
// replicator for data synchronization, gRPC server for client communication,
// and membership for cluster discovery.
type Agent struct {
	Config

	log        *log.Log
	replicator *log.Replicator
	server     *grpc.Server
	membership *discovery.Membership

	shutdown     bool
	shutdowns    chan struct{}
	shutdownLock sync.Mutex
}

// Config holds the configuration parameters for the distributed log agent.
// It contains settings for TLS encryption, networking, data storage, clustering,
// and access control functionality.
// ServerTLSConfig specifies the TLS configuration for server connections.
// PeerTLSConfig specifies the TLS configuration for peer-to-peer communication.
// DataDir is the directory path where the agent stores its data files.
// BindAddr is the network address the agent binds to for incoming connections.
// RPCPort is the port number used for RPC communication.
// NodeName is the unique identifier for this agent node in the cluster.
// StartJoinAddrs contains the addresses of existing nodes to join when starting.
// ACLModelFile is the file path to the access control model configuration.
// ACLPolicyFile is the file path to the access control policy definitions.
type Config struct {
	ServerTLSConfig *tls.Config
	PeerTLSConfig   *tls.Config
	DataDir         string
	BindAddr        string
	RPCPort         int
	NodeName        string
	StartJoinAddrs  []string
	ACLModelFile    string
	ACLPolicyFile   string
}

func (c Config) RPCAddr() (string, error)  {
	host, _, err := net.SplitHostPort(c.BindAddr)
	if err != nil {
		return "", nil
	}
	return fmt.Sprintf("%s:%d", host, c.RPCPort), nil
}

func New(c Config) (*Agent, error) {
	a := &Agent{
		Config: c,
		shutdowns: make(chan struct{}),
	}

	setup := []func() error {
		a.setupLogger,
		a.setupLog,
		a.setupMembership,
		a.setupServer,
	}

	for _, fn := range setup {
		if err := fn(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *Agent) setupLogger() error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	zap.ReplaceGlobals(logger)
	return nil
}

func (a *Agent) setupLog() error {
	var err error
	a.log, err = log.NewLog(a.Config.DataDir, log.Config{})
	if err != nil {
		return err
	}
	return nil
}

// setupServer initializes and starts the gRPC server for the Agent.
// It creates an authorizer using the configured ACL model and policy files,
// sets up server configuration with the commit log and authorizer,
// applies TLS credentials if configured, creates a new gRPC server,
// starts listening on the RPC address, and serves the gRPC server in a goroutine.
// If the server fails to serve, it triggers a shutdown of the Agent.
// Returns an error if any step in the setup process fails.
func (a *Agent) setupServer() error {
	authorizer := auth.New(a.Config.ACLModelFile, a.Config.ACLPolicyFile)
	serverConfig := &server.Config{
		CommitLog: a.log,
		Authorizer: authorizer,
	}

	var opts []grpc.ServerOption
	if a.Config.ServerTLSConfig != nil {
		creds := credentials.NewTLS(a.Config.ServerTLSConfig)
		opts = append(opts, grpc.Creds(creds))
	}

	var err error
	a.server, err  = server.NewGRPCServer(serverConfig, opts...)
	if err != nil {
		return err
	}
	rpcAddr, err := a.RPCAddr()
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", rpcAddr) 
	if err != nil {
		return err
	}

	go func() {
		if err := a.server.Serve(ln); err != nil { 
			_ = a.Shutdown()
		} 
	}()
	return err
}

// setupMembership initializes the agent's cluster membership and replication components.
// It establishes a gRPC connection to the local RPC server, configures TLS if specified,
// creates a replicator for log synchronization, and sets up service discovery with the
// provided node configuration including name, bind address, RPC address tags, and
// initial join addresses for cluster bootstrapping.
func (a *Agent) setupMembership() error {
	rpcAddr, err := a.Config.RPCAddr()
	if err != nil {
		return err
	}

	var opts []grpc.DialOption
	if a.Config.PeerTLSConfig != nil {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(a.Config.PeerTLSConfig)))
	}

	conn, err := grpc.NewClient(rpcAddr, opts...)
	if err != nil {
		return err
	}
	client := api.NewLogClient(conn)
	a.replicator = &log.Replicator{
		DialOptions: opts,
		LocalServer: client,
	}

	a.membership, err = discovery.New(a.replicator, discovery.Config{
		NodeName: a.Config.NodeName,
		BindAddr: a.Config.BindAddr,
		Tags: map[string]string{
			"rpc_addr": rpcAddr,
		},
		StartJoinAddrs: a.Config.StartJoinAddrs,
	})
	return err
}

// Shutdown gracefully shuts down the agent by stopping all its components in order.
// It ensures that the shutdown process only happens once by using a shutdown flag and lock.
// The shutdown sequence stops the membership service, replicator, gRPC server, and log
// in that specific order. Returns an error if any component fails to shut down properly.
func (a *Agent) Shutdown() error {
	a.shutdownLock.Lock()
	defer a.shutdownLock.Unlock()
	if a.shutdown {
		return nil
	}

	a.shutdown = true
	close(a.shutdowns)

	shutdown := []func() error {
		a.membership.Leave,
		a.replicator.Close,
		func() error { 
			a.server.GracefulStop()
			return nil
		}, 
		a.log.Close,
	}

	for _, fn := range shutdown {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

