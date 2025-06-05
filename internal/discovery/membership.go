package discovery

import (
	"net"

	"github.com/hashicorp/serf/serf"
	"go.uber.org/zap"
)

type Membership struct {
	Config
	handler Handler
	serf *serf.Serf
	events chan serf.Event
	logger *zap.Logger
}

type Handler interface {
	Join(name, addr string) error
	Leave(name string) error
}

func New(handler Handler, config Config) (*Membership, error) {
	c := &Membership{
		Config: config,
		handler: handler,
		logger: zap.L().Named("membership"),
	}
	if err := c.setupSerf(); err != nil {
		return nil, err
	}
	return c, nil
}

type Config struct {
	NodeName string
	BindAddr string
	Tags map[string]string
	StartJoinAddrs []string	
}

// setupSerf initializes and configures the Serf instance for cluster membership management.
// It creates a Serf configuration with the specified bind address and port,
// sets up an eventchannel for handling cluster events
// applies any custom tags and node name from the membership configuration. 
// After creating the Serf instance, it starts an event handlergoroutine and 
// attempts to join existing cluster nodes if StartJoinAddrs is provided.
// Returns an error if any step of the setup process fails.
func (m *Membership) setupSerf() error {

	addr, err := net.ResolveTCPAddr("tcp", m.BindAddr)
	if err != nil {
		return err
	}

	config := serf.DefaultConfig()
	config.Init()
	config.MemberlistConfig.BindAddr = addr.IP.String()
	config.MemberlistConfig.BindPort = addr.Port
	m.events = make(chan serf.Event)
	config.EventCh = m.events
	config.Tags = m.Config.Tags
	config.NodeName = m.Config.NodeName
	
	m.serf, err = serf.Create(config)
	if err != nil {
		return err
	}

	go m.eventHandler()
	if m.StartJoinAddrs != nil {
		_, err := m.serf.Join(m.StartJoinAddrs, true)
		if err != nil {
			return err
		}
	}
	return nil
}

// eventHandler processes membership events from the Serf cluster in a continuous loop.
// It handles three types of events:
// - EventMemberJoin: calls handleJoin for each non-local member that joins the cluster
// - EventMemberLeave: calls handleLeave for each non-local member that leaves the cluster
// - EventMemberFailed: calls handleLeave for each non-local member that fails/becomes unreachable
// Local members (the current node) are ignored for all event types to prevent self-processing.
// This method runs as a goroutine and blocks until the events channel is closed.
func (m *Membership) eventHandler() {
	for e := range m.events {
		switch e.EventType() {
		case serf.EventMemberJoin:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					continue
				}
				m.handleJoin(member)
			}
		
		case serf.EventMemberLeave, serf.EventMemberFailed:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					return
				}
				m.handleLeave(member)
			}
		}
	}
}

func (m *Membership) handleJoin(member serf.Member) {
	if err := m.handler.Join(member.Name, member.Tags["rpc_addr"]); err != nil {
		m.logError(err, "failed to join", member)
	}
}

func (m *Membership) handleLeave(member serf.Member) {
	if err := m.handler.Leave(member.Name); err != nil {
		m.logError(err, "failed to leave", member)
	}
}

func (m *Membership) isLocal(member serf.Member) bool {
	return m.serf.LocalMember().Name == member.Name
}

func (m *Membership) logError(err error, msg string, member serf.Member) {
	m.logger.Error(msg, zap.Error(err), zap.String("name", member.Name), zap.String("rpc_addr", member.Tags["rpc_addr"]))
}

func (m *Membership) Members() []serf.Member {
	return m.serf.Members()
}

func (m *Membership) Leave() error {
	return m.serf.Leave()
}