package agent

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"github.com/palSagnik/aphros/internal/auth"
	"github.com/palSagnik/aphros/internal/discovery"
	"github.com/palSagnik/aphros/internal/log"
	"github.com/palSagnik/aphros/internal/server"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

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

