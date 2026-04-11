package state

import (
	"sort"
	"time"
)

// ConnectorState is the per-connector private state value. Each
// connector impl defines its own concrete type (e.g. connector.GitHubState)
// by embedding ConnectorStateBase. ConnectorState values are stored in
// State.Connectors and round-tripped through Reduce without inspection.
//
// The marker method is unexported to seal the interface to types that
// embed ConnectorStateBase.
type ConnectorState interface {
	connectorStateMarker()
}

// ConnectorStateBase is the embed-only marker that promotes a struct
// into a valid ConnectorState.
type ConnectorStateBase struct{}

func (ConnectorStateBase) connectorStateMarker() {}

// ConnectorEvent is the closed sum type the reducer hands to a
// Connector's Step method.
type ConnectorEvent interface {
	isConnectorEvent()
}

// CEvTick is the periodic tick. Connectors use it to decide whether
// to schedule a fetch job (e.g. check cache TTL).
type CEvTick struct {
	Now time.Time
}

func (CEvTick) isConnectorEvent() {}

// CEvJobResult delivers an async worker pool result back to the
// connector that requested it.
type CEvJobResult struct {
	Result any
	Err    error
	Now    time.Time
}

func (CEvJobResult) isConnectorEvent() {}

// ConnectorView is the TUI payload a connector produces. The TUI
// renders all connectors generically — no connector-name branching.
type ConnectorView struct {
	Label     string             `json:"label"`
	Summary   string             `json:"summary"`
	Available bool               `json:"available"`
	Sections  []ConnectorSection `json:"sections,omitempty"`
}

// ConnectorSection is a titled group of items in the main TUI display.
type ConnectorSection struct {
	Title string          `json:"title"`
	Items []ConnectorItem `json:"items,omitempty"`
}

// ConnectorItem is one entry within a ConnectorSection.
type ConnectorItem struct {
	Symbol string `json:"symbol"`
	Title  string `json:"title"`
	Meta   string `json:"meta"`
}

// Connector is the interface every daemon-level connector plugin
// implements. Each impl is a stateless value type registered once at
// init time; the per-connector state lives in ConnectorState values
// returned by NewState.
type Connector interface {
	// Name is the registry key (e.g. "github").
	Name() string

	// DisplayName is the human-readable label shown in TUI.
	DisplayName() string

	// NewState constructs a fresh ConnectorState.
	NewState() ConnectorState

	// Step is the per-connector reducer. Pure function: no I/O,
	// no goroutines. All side effects are returned as []Effect.
	Step(prev ConnectorState, ev ConnectorEvent) (ConnectorState, []Effect)

	// View is a pure getter for the current TUI payload.
	View(s ConnectorState) ConnectorView
}

// connector registry. Set once at init time by each connector impl.
var connectorRegistry = map[string]Connector{}

// RegisterConnector adds a Connector to the registry under its Name().
// Panics on duplicate names.
func RegisterConnector(c Connector) {
	name := c.Name()
	if _, exists := connectorRegistry[name]; exists {
		panic("state: duplicate connector registration: " + name)
	}
	connectorRegistry[name] = c
}

// GetConnector returns the Connector for the given name, or nil.
func GetConnector(name string) Connector {
	return connectorRegistry[name]
}

// AllConnectors returns all registered connectors sorted by name
// for stable iteration order.
func AllConnectors() []Connector {
	if len(connectorRegistry) == 0 {
		return nil
	}
	names := make([]string, 0, len(connectorRegistry))
	for name := range connectorRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Connector, len(names))
	for i, name := range names {
		out[i] = connectorRegistry[name]
	}
	return out
}
