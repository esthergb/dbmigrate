package binlog

import "fmt"

type applyFailure struct {
	FailureType string
	File        string
	Pos         uint32
	Operation   string
	TableName   string
	Query       string
	Remediation string
	Message     string
	Cause       error
	AppliedFile string
	AppliedPos  uint32
}

func (f *applyFailure) Error() string {
	if f == nil {
		return "replication apply failure"
	}
	base := f.Message
	if base == "" {
		base = "replication apply failure"
	}
	if f.Cause != nil {
		return fmt.Sprintf("%s: %v", base, f.Cause)
	}
	return base
}
