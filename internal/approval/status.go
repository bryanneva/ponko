package approval

// Status is a typed approval gate result.
type Status int

// Status constants for the approval gate.
const (
	Pending  Status = 0
	Approved Status = 1
	Rejected Status = 2
)
