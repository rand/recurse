package rlmcore

import "errors"

// ErrNotAvailable is returned when rlm-core bindings are not available.
var ErrNotAvailable = errors.New("rlm-core not available")
