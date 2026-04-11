package state

import "testing"

type stubConnectorState struct {
	ConnectorStateBase
	Val int
}

type stubConnector struct {
	name string
}

func (s stubConnector) Name() string        { return s.name }
func (s stubConnector) DisplayName() string { return s.name }
func (s stubConnector) NewState() ConnectorState {
	return stubConnectorState{}
}
func (s stubConnector) Step(prev ConnectorState, ev ConnectorEvent) (ConnectorState, []Effect) {
	cs := prev.(stubConnectorState)
	cs.Val++
	return cs, nil
}
func (s stubConnector) View(st ConnectorState) ConnectorView {
	cs := st.(stubConnectorState)
	return ConnectorView{Label: s.name, Available: cs.Val > 0}
}

func TestRegisterAndGetConnector(t *testing.T) {
	// Save and restore registry.
	orig := connectorRegistry
	connectorRegistry = map[string]Connector{}
	defer func() { connectorRegistry = orig }()

	RegisterConnector(stubConnector{name: "test"})

	if got := GetConnector("test"); got == nil {
		t.Fatal("GetConnector returned nil for registered connector")
	}
	if got := GetConnector("missing"); got != nil {
		t.Fatal("GetConnector returned non-nil for unregistered connector")
	}
}

func TestRegisterConnectorPanicsOnDuplicate(t *testing.T) {
	orig := connectorRegistry
	connectorRegistry = map[string]Connector{}
	defer func() { connectorRegistry = orig }()

	RegisterConnector(stubConnector{name: "dup"})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	RegisterConnector(stubConnector{name: "dup"})
}

func TestAllConnectorsSortedByName(t *testing.T) {
	orig := connectorRegistry
	connectorRegistry = map[string]Connector{}
	defer func() { connectorRegistry = orig }()

	RegisterConnector(stubConnector{name: "zeta"})
	RegisterConnector(stubConnector{name: "alpha"})
	RegisterConnector(stubConnector{name: "mid"})

	all := AllConnectors()
	if len(all) != 3 {
		t.Fatalf("AllConnectors length = %d, want 3", len(all))
	}
	if all[0].Name() != "alpha" || all[1].Name() != "mid" || all[2].Name() != "zeta" {
		t.Errorf("AllConnectors order = [%s, %s, %s], want [alpha, mid, zeta]",
			all[0].Name(), all[1].Name(), all[2].Name())
	}
}

func TestAllConnectorsEmpty(t *testing.T) {
	orig := connectorRegistry
	connectorRegistry = map[string]Connector{}
	defer func() { connectorRegistry = orig }()

	if got := AllConnectors(); got != nil {
		t.Errorf("AllConnectors = %v, want nil", got)
	}
}
