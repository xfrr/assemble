package assemble

import "errors"

// MultiError aggregates multiple errors into one.
type MultiError struct {
	errs []error
}

func (m *MultiError) Append(err error) {
	if err == nil {
		return
	}
	m.errs = append(m.errs, err)
}

func (m *MultiError) Len() int { return len(m.errs) }

func (m *MultiError) Error() string {
	if len(m.errs) == 0 {
		return ""
	}
	if len(m.errs) == 1 {
		return m.errs[0].Error()
	}
	s := "multiple errors:"
	for _, e := range m.errs {
		s += "\n  - " + e.Error()
	}
	return s
}

// TreeString renders a simple indented list of aggregated errors.
func (m *MultiError) TreeString(indent string) string {
	if len(m.errs) == 0 {
		return ""
	}
	if len(m.errs) == 1 {
		return indent + "- " + m.errs[0].Error()
	}
	s := ""
	for i, e := range m.errs {
		if i == len(m.errs)-1 {
			s += indent + "└─ " + e.Error() + "\n"
		} else {
			s += indent + "├─ " + e.Error() + "\n"
		}
	}
	return s
}

func (m *MultiError) Unwrap() []error { return m.errs }

// IsMultiError returns true if the error is a MultiError.
// It uses errors.As to unwrap the error chain.
func IsMultiError(err error) (*MultiError, bool) {
	var me *MultiError
	return me, errors.As(err, &me)
}

// isNotFound returns true if the error is a NotFoundError.
// It uses errors.As to unwrap the error chain.
func isNotFound(err error) bool {
	var nf NotFoundError
	return errors.As(err, &nf)
}
