package store

import "errors"

// ErrUnsupported indicates an operation is not supported by the current store
// implementation. This is used by filestore (single-user personal mode) for
// interfaces that only make sense in platform mode, such as the content-
// addressed sandbox layer store and the cross-pod chat event journal.
//
// Callers can distinguish "feature not applicable in this mode" from other
// errors via errors.Is(err, store.ErrUnsupported). Implementations that return
// this error MUST return it verbatim (not wrapped) so the equality check holds.
var ErrUnsupported = errors.New("operation not supported by this store implementation")
