package subscription

import "fmt"

type DraftRowKind string

const (
	NodeRow DraftRowKind = "节点"
	RuleRow DraftRowKind = "规则"
)

// RowValidationError identifies an invalid node or rule by its one-based position.
type RowValidationError struct {
	Kind   DraftRowKind
	Number int
	err    error
}

func (e *RowValidationError) Error() string {
	return fmt.Sprintf("%s %d：%s", e.Kind, e.Number, e.err)
}

func (e *RowValidationError) Unwrap() error { return e.err }
