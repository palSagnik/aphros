package loadbalance

import (
	"sync"

	api "github.com/palSagnik/aphros/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

type Resolver struct {
	mu sync.Mutex
	clientConn resolver.ClientConn
	resolver *grpc.ClientConn
	serviceConfig *serviceconfig.ParseResult
	logger *zap.Logger
}

