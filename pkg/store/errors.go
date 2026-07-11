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

// ErrBundledTemplateImmutable indicates an attempt to save or delete an
// Astonish-shipped (embedded) fleet template key. Callers should clone to a
// new key instead. Check with errors.Is(err, store.ErrBundledTemplateImmutable).
var ErrBundledTemplateImmutable = errors.New("bundled fleet template is immutable; clone to a new key to customize")

// ErrBundledSetupProfileImmutable indicates an attempt to save or delete a bundled setup profile.
var ErrBundledSetupProfileImmutable = errors.New("bundled setup profile is immutable; clone to a new key to customize")
