package routing

import "strings"

// Complexity classifies how difficult a ticket is. Determines which
// model handles it and how deep the discussion goes.
type Complexity int

const (
	// Trivial is a simple change: rename, typo, docs, chore. Uses Haiku.
	Trivial Complexity = iota
	// Standard is a normal feature or bug fix. Uses Sonnet.
	Standard
	// Complex is an architectural change, security fix, or migration. Uses Opus.
	Complex
)

// String returns the complexity name.
func (c Complexity) String() string {
	switch c {
	case Trivial:
		return "trivial"
	case Standard:
		return "standard"
	case Complex:
		return "complex"
	}
	return "unknown"
}

// ComplexityClassifier maps ticket metadata to complexity levels.
type ComplexityClassifier struct {
	models map[Complexity]string
}

// NewComplexityClassifier creates a classifier with model mappings.
func NewComplexityClassifier(trivialModel, standardModel, complexModel string) *ComplexityClassifier {
	return &ComplexityClassifier{
		models: map[Complexity]string{
			Trivial:  trivialModel,
			Standard: standardModel,
			Complex:  complexModel,
		},
	}
}

// Classify determines the complexity of a ticket from its metadata.
// No LLM calls — pure heuristics.
func (cc *ComplexityClassifier) Classify(title, description string, labels []string) Complexity {
	if isComplex(title, description, labels) {
		return Complex
	}

	if isTrivial(title, description, labels) {
		return Trivial
	}

	return Standard
}

// ModelFor returns the model ID for the given complexity.
func (cc *ComplexityClassifier) ModelFor(complexity Complexity) string {
	model, ok := cc.models[complexity]
	if !ok {
		return cc.models[Standard]
	}
	return model
}

var trivialLabels = map[string]bool{
	"chore": true, "docs": true, "style": true, "rename": true,
	"typo": true, "cleanup": true, "housekeeping": true,
}

var complexLabels = map[string]bool{
	"architecture": true, "security": true, "migration": true,
	"epic": true, "breaking": true, "infrastructure": true,
	"performance": true,
}

func isTrivial(title, description string, labels []string) bool {
	for _, label := range labels {
		if trivialLabels[strings.ToLower(label)] {
			return true
		}
	}

	// Short descriptions signal simple work.
	combined := title + " " + description
	return len(combined) < 200
}

func isComplex(_, description string, labels []string) bool {
	for _, label := range labels {
		if complexLabels[strings.ToLower(label)] {
			return true
		}
	}

	// Long descriptions signal complex work.
	return len(description) > 1000
}
