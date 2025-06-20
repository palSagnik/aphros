package loadbalance_test

import (
	"net"
	"net/url"
	"testing"

	api "github.com/palSagnik/aphros/api/v1"
	"github.com/palSagnik/aphros/internal/config"
	"github.com/palSagnik/aphros/internal/loadbalance"
	"github.com/palSagnik/aphros/internal/server"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

func TestResolver(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// server set-up
	tlsConfig, err := config.SetUpTLSConfig(config.TLSConfig{
		CAFile:        config.CAFile,
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		Server:        true,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)
	serverCreds := credentials.NewTLS(tlsConfig)

	srv, err := server.NewGRPCServer(&server.Config{GetServerer: &getServers{}}, grpc.Creds(serverCreds))
	require.NoError(t, err)

	go srv.Serve(l)

	// client set up
	conn := &clientConn{}
	tlsConfig, err = config.SetUpTLSConfig(config.TLSConfig{
		CAFile:        config.CAFile,
		CertFile:      config.RootClientCertFile,
		KeyFile:       config.RootClientKeyFile,
		Server:        false,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)
	clientCreds := credentials.NewTLS(tlsConfig)

	opts := resolver.BuildOptions{
		DialCreds: clientCreds,
	}
	r := &loadbalance.Resolver{}
	_, err = r.Build(resolver.Target{URL: url.URL{Host: l.Addr().String()}}, conn, opts)
	require.NoError(t, err)

	wantState := resolver.State{
		Addresses: []resolver.Address{
			{Addr:  "localhost:9001", Attributes: attributes.New("is_leader", true)}, 
			{Addr:  "localhost:9002", Attributes: attributes.New("is_leader", false)},
		},
	}
	require.Equal(t, wantState, conn.state)

	conn.state.Addresses = nil
	r.ResolveNow(resolver.ResolveNowOptions{})
	require.Equal(t, wantState, conn.state)

}

type getServers struct{}

// getServers implements GetServerers, whose job is to return a known server set for the resolver to find.
func (s *getServers) GetServers() ([]*api.Server, error) {
	return []*api.Server{
		{Id: "leader",	RpcAddr: "localhost:9001", IsLeader: true}, 
		{Id: "follower", RpcAddr: "localhost:9002"},
	}, nil
}

type clientConn struct {
	resolver.ClientConn
	state resolver.State
}

func (c *clientConn) UpdateState(state resolver.State) error { 
	c.state = state
	return nil
}

func (c *clientConn) ReportError(err error) {}

func (c *clientConn) NewAddress(addrs []resolver.Address) {} 

func (c *clientConn) NewServiceConfig(config string) {}

func (c *clientConn) ParseServiceConfig(config string) *serviceconfig.ParseResult { 
	return nil
}
