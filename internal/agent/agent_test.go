package agent_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"testing"
	"time"

	api "github.com/palSagnik/aphros/api/v1"
	"github.com/palSagnik/aphros/internal/agent"
	"github.com/palSagnik/aphros/internal/config"
	"github.com/palSagnik/aphros/internal/loadbalance"
	"github.com/stretchr/testify/require"
	"github.com/travisjeffery/go-dynaport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

func TestAgent(t *testing.T) {
	serverTLSConfig, err := config.SetUpTLSConfig(config.TLSConfig{
		CertFile: config.ServerCertFile,
		KeyFile: config.ServerKeyFile,
		CAFile: config.CAFile,
		Server: true,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)

	peerTLSConfig, err := config.SetUpTLSConfig(config.TLSConfig{
		CertFile: config.RootClientCertFile,
		KeyFile: config.RootClientKeyFile,
		CAFile: config.CAFile,
		Server: false,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)

	var agents []*agent.Agent
	for i := 0; i < 3; i++ {
		ports := dynaport.Get(2)
		bindAddr := fmt.Sprintf("%s:%d", "127.0.0.1", ports[0])
		rpcPort := ports[1]

		dataDir, err := os.MkdirTemp("", "agent-test-log")
		require.NoError(t, err)

		var startJoinAddrs []string
		if i != 0 {
			startJoinAddrs = append(startJoinAddrs, agents[0].Config.BindAddr)
		}

		agent, err := agent.New(agent.Config{
			NodeName: fmt.Sprintf("%d", i),
			StartJoinAddrs: startJoinAddrs,
			BindAddr: bindAddr,
			RPCPort: rpcPort,
			DataDir: dataDir,
			ACLModelFile: config.ACLModelFile,
			ACLPolicyFile: config.ACLPolicyFile,
			ServerTLSConfig: serverTLSConfig,
			PeerTLSConfig: peerTLSConfig,
			Bootstrap: i == 0,
		})
		require.NoError(t, err)

		agents = append(agents, agent)
	}
	defer func() {
		for _, agent := range agents {
			err := agent.Shutdown()
			require.NoError(t, err)
			require.NoError(t, os.RemoveAll(agent.Config.DataDir))
		}
	}()
	time.Sleep(3 * time.Second)

	// Setup done
	// Testing Produce and Consume
	leaderClient := client(t, agents[0], peerTLSConfig)
	produceResponse, err := leaderClient.Produce(
		context.Background(),
		&api.ProduceRequest{
			Record: &api.Record{
				Value: []byte("hello-world-agent-testing"),
			},
		},
	)
	require.NoError(t, err)

	time.Sleep(3 * time.Second)  // wait until replication has finished
	
	consumeResponse, err := leaderClient.Consume(
		context.Background(),
		&api.ConsumeRequest{
			Offset: produceResponse.Offset,
		},
	)
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Record.Value, []byte("hello-world-agent-testing"))

	followerClient := client(t, agents[1], peerTLSConfig)
	consumeResponse, err = followerClient.Consume(
		context.Background(),
		&api.ConsumeRequest{
			Offset: produceResponse.Offset,
		},
	)
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Record.Value, []byte("hello-world-agent-testing"))

	consumeResponse, err = leaderClient.Consume(
		context.Background(),
		&api.ConsumeRequest{Offset: produceResponse.Offset + 1},
	)
	require.Nil(t, consumeResponse)
	require.Error(t, err)
	
	got := status.Code(err)
	want := status.Code(api.ErrOffsetOutOfRange{}.GRPCStatus().Err())
	require.Equal(t, got, want)
}

func client(t *testing.T, agent *agent.Agent, tlsConfig *tls.Config)  api.LogClient {
	t.Helper()
	tlsCreds := credentials.NewTLS(tlsConfig)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(tlsCreds)}
	
	rpcAddr, err := agent.Config.RPCAddr()
	require.NoError(t, err)

	conn, err := grpc.NewClient(fmt.Sprintf("%s://%s", loadbalance.Name, rpcAddr), opts...)
	require.NoError(t, err)

	client := api.NewLogClient(conn)
	return client
}