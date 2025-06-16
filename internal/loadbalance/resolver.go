package loadbalance

import (
	"sync"

	api "github.com/palSagnik/aphros/api/v1"
)

type Resolver struct {
	mu sync.Mutex
}
