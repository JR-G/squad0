package routing

// Depth controls how much team discussion happens before implementation.
type Depth int

const (
	// DepthNone skips discussion entirely. For trivial tickets.
	DepthNone Depth = iota
	// DepthLight runs only the Tech Lead review. For standard tickets.
	DepthLight
	// DepthFull runs the complete discussion ritual: plan, tech lead,
	// team debate, PM tie-break. For complex tickets.
	DepthFull
)

// String returns the depth name.
func (d Depth) String() string {
	switch d {
	case DepthNone:
		return "none"
	case DepthLight:
		return "light"
	case DepthFull:
		return "full"
	}
	return "unknown"
}

// ClassifyDepth maps complexity to discussion depth.
func ClassifyDepth(complexity Complexity) Depth {
	switch complexity { //nolint:exhaustive // Standard is the default
	case Trivial:
		return DepthNone
	case Complex:
		return DepthFull
	default:
		return DepthLight
	}
}
