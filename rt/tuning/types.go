package tuning

import "time"

// Source indicates where the current effective value comes from.
type Source int

const (
	SourceDefault Source = iota
	SourceRuntimeSet
)

func (s Source) String() string {
	switch s {
	case SourceDefault:
		return "default"
	case SourceRuntimeSet:
		return "runtime-set"
	default:
		return "unknown"
	}
}

// Type indicates a tuning variable type.
type Type string

const (
	TypeBool     Type = "bool"
	TypeInt64    Type = "int64"
	TypeFloat64  Type = "float64"
	TypeString   Type = "string"
	TypeDuration Type = "duration"
	TypeEnum     Type = "enum"
)

// Constraints is a summary of validations attached to a variable.
//
// It is primarily for Snapshot / ExportOverrides and should remain stable.
type Constraints struct {
	Min *string `json:"min,omitempty"`
	Max *string `json:"max,omitempty"`

	NonEmpty bool `json:"nonEmpty,omitempty"`

	EnumAllowed []string `json:"enumAllowed,omitempty"`
}

// Item is a point-in-time view of a single variable.
type Item struct {
	Key string `json:"key"`

	Type Type `json:"type"`

	// Value is the current effective value.
	// If the variable is redacted, Value is "<redacted>".
	Value any `json:"value"`

	// DefaultValue is the registered default value.
	// If the variable is redacted, DefaultValue is "<redacted>".
	DefaultValue any `json:"defaultValue"`

	Source Source `json:"source"`

	// LastUpdatedAt is the timestamp of the last successful runtime write (Set/Reset*).
	// Zero means never updated.
	LastUpdatedAt time.Time `json:"lastUpdatedAt"`

	Constraints Constraints `json:"constraints"`
}

// Snapshot is a view of all registered variables.
type Snapshot struct {
	Items []Item `json:"items"`
}

// OverrideItem is an exported override record for ops workflows.
//
// Value is always a JSON string that should be parsed by type:
//   - bool: "true" / "false"
//   - int64: base10
//   - float64: decimal string
//   - string: raw string (JSON-escaped)
//   - duration: Go duration string (e.g. "800ms")
//   - enum: raw string (must be in allowed)
type OverrideItem struct {
	Key   string `json:"key"`
	Type  Type   `json:"type"`
	Value string `json:"value"`
}
