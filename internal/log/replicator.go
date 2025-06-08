package log

import (
	"context"
	"sync"

	api "github.com/palSagnik/aphros/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// Replicator manages the replication of log entries across multiple servers in a distributed system.
// It maintains connections to remote servers and coordinates the synchronization of log data
// between the local server and remote replicas. The Replicator handles connection management,
// tracks server availability, and ensures data consistency across the cluster.
//
// Fields:
//   - DialOptions: gRPC dial options used when establishing connections to remote servers
//   - LocalServer: Client interface to interact with the local log server
//   - logger: Structured logger for debugging and monitoring replication activities
//   - mu: Mutex protecting concurrent access to the servers map and closed flag
//   - servers: Map tracking active replication channels for each server address
//   - closed: Flag indicating whether the replicator has been shut down
//   - close: Channel used to signal shutdown to all running replication goroutines
type Replicator struct {
	DialOptions []grpc.DialOption
	LocalServer api.LogClient

	logger *zap.Logger

	mu sync.Mutex
	servers map[string]chan struct{}
	closed bool
	close chan struct{}
}

func (r *Replicator) Join(name, addr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()

	if r.closed {
		return nil
	}

	if _, ok := r.servers[name]; ok {
		// already replicating server, so skip
		return nil
	}

	r.servers[name] = make(chan struct{})
	go r.replicate(addr, r.servers[name])

	return nil
}

// replicate establishes a gRPC connection to the specified address and continuously
// replicates records from the remote server to the local server. It creates a
// consume stream to receive records from the remote server and produces them
// locally. The function runs until it receives a signal on either the replicator's
// close channel or the provided leave channel. If any errors occur during
// connection, streaming, or production, they are logged and the function returns.
//
// Parameters:
//   - addr: The address of the remote server to replicate from
//   - leave: A channel that signals when replication should stop for this address
func (r *Replicator) replicate(addr string, leave chan struct{}) {
	cc, err := grpc.NewClient(addr, r.DialOptions...)
	if err != nil {
		r.logError(err, "failed to dial", addr)
		return
	}
	defer cc.Close()

	client := api.NewLogClient(cc)

	ctx := context.Background()
	stream, err := client.ConsumeStream(ctx, &api.ConsumeRequest{Offset: 0})
	if err != nil {
		r.logError(err, "failed to consume", addr)
		return
	}

	records := make(chan *api.Record)
	go func() {
		for {
			recv, err := stream.Recv()
			if err != nil {
				r.logError(err, "failed to receive", addr)
				return
			}
			records <- recv.Record
		}
	}()

	for {
		select {
		case <- r.close:
			return
		case <- leave:
			return
		case record := <-records:
			_, err := r.LocalServer.Produce(ctx, &api.ProduceRequest{Record: record})
			if err != nil {
				r.logError(err, "failed to produce", addr)
				return
			}
		}
	}
}

func (r *Replicator) Leave(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	
	if _, ok := r.servers[name]; !ok {
		return nil
	}

	close(r.servers[name])
	delete(r.servers, name)
	return nil
}


func (r *Replicator) init() { 
	if r.logger == nil {
		r.logger = zap.L().Named("replicator") 
	}
	if r.servers == nil {
		r.servers = make(map[string]chan struct{}) 
	}
	if r.close == nil {
		r.close = make(chan struct{})
	}
}

func (r *Replicator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()

	if r.closed {
		return nil
	}

	r.closed = true
	close(r.close)
	return nil
}

func (r *Replicator) logError(err error, msg, addr string) {
	r.logger.Error(msg, zap.String("addr", addr), zap.Error(err))
}