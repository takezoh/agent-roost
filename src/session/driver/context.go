package driver

// SessionContext is the driver-visible projection of a session. core/manager
// constructs this when calling Driver methods so that drivers never touch the
// full session.Session struct (which would create an import cycle and expose
// fields drivers have no business reading).
//
// DriverState is a map[string]string whose keys are defined by the driver
// itself; core treats them as opaque.
type SessionContext struct {
	Command     string
	Project     string
	DriverState map[string]string
}
