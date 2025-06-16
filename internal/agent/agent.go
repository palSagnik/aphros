package agent

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"github.com/palSagnik/aphros/internal/auth"
	"github.com/palSagnik/aphros/internal/discovery"
	"github.com/palSagnik/aphros/internal/log"
	"github.com/palSagnik/aphros/internal/server"

	"github.com/soheilhy/cmux"
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

	mux        cmux.CMux
	log        *log.DistributedLog
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
	Bootstrap       bool
}

func (c Config) RPCAddr() (string, error) {
	host, _, err := net.SplitHostPort(c.BindAddr)
	if err != nil {
		return "", nil
	}
	return fmt.Sprintf("%s:%d", host, c.RPCPort), nil
}

// setupMux initializes the connection multiplexer for the agent.
// It creates a TCP listener on the configured RPC port and sets up
// a connection multiplexer (cmux) to handle multiple protocols
// on the same port. Returns an error if the TCP listener fails to start.
func (a *Agent) setupMux() error {
	rpcAddr := fmt.Sprintf(":%d", a.Config.RPCPort)

	ln, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return err
	}

	a.mux = cmux.New(ln)
	return nil
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
	raftLn := a.mux.Match(func (reader io.Reader) bool {
		b := make([]byte, 1)
		if _, err := reader.Read(b); err != nil {
			return false
		}
		return bytes.Equal(b, []byte{byte(log.RaftRPC)})
	})

	logConfig := log.Config{}
	logConfig.Raft.StreamLayer = log.NewStreamLayer(raftLn, a.Config.ServerTLSConfig, a.Config.PeerTLSConfig)
	logConfig.Raft.LocalID = raft.ServerID(a.Config.NodeName)
	logConfig.Raft.Bootstrap = a.Config.Bootstrap

	var err error
	a.log, err = log.NewDistributedLog(a.Config.DataDir, logConfig)
	if err != nil {
		return err
	}

	if a.Config.Bootstrap {
		err = a.log.WaitForLeader(3 * time.Second)
	}
	return err
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
		GetServerer: a.log,
	}

	grpcServerOpts := []grpc.ServerOption{}
	if a.Config.ServerTLSConfig != nil {
		creds := credentials.NewTLS(a.Config.ServerTLSConfig)
		grpcServerOpts = append(grpcServerOpts, grpc.Creds(creds))
	}

	var err error
	a.server, err = server.NewGRPCServer(serverConfig, grpcServerOpts...)
	if err != nil {
		return err
	}

	grpcLn := a.mux.Match(cmux.Any())
	go func() {
		if err := a.server.Serve(grpcLn); err != nil {
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

	a.membership, err = discovery.New(a.log, discovery.Config{
		NodeName:       a.Config.NodeName,
		BindAddr:       a.Config.BindAddr,
		Tags:           map[string]string{"rpc_addr": rpcAddr},
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

	shutdown := []func() error{
		a.membership.Leave,
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

func (a *Agent) serve() error {
	if err := a.mux.Serve(); err != nil {
		_ = a.Shutdown() 
		return err
	}
	return nil
}

func New(c Config) (*Agent, error) {
	agent := &Agent{
		Config:    c,
		shutdowns: make(chan struct{}),
	}

	setupFuncs := []func() error{
		agent.setupLogger,
		agent.setupMux,
		agent.setupLog,
		agent.setupServer,
		agent.setupMembership,
	}

	for _, fn := range setupFuncs {
		if err := fn(); err != nil {
			return nil, err
		}
	}
	
	go agent.serve()
	return agent, nil
}
