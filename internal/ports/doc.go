// Package ports defines the abstract contracts squad0 expects from
// external services (PR host, ticket source, chat platform). Concrete
// implementations live under internal/integrations/<provider> and
// satisfy the port interfaces declared here.
//
// This is a hexagonal-architecture / ports-and-adapters layout. The
// port (interface) is owned by the domain side (squad0); the adapter
// (implementation) is owned by the integration side. Call sites
// depend on the port, not the adapter, so swapping providers is a
// constructor change rather than a code change.
//
// As of the initial extraction the call sites in internal/orchestrator
// still touch concrete adapters directly. Migration to the ports is
// per-site follow-up — declaring the contracts and providing wrappers
// is the foundation; swapping each call to depend on the interface is
// independent work that doesn't risk a big-bang breakage.
package ports
