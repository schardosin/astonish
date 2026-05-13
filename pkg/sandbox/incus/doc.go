// Package incus contains the Incus-backed sandbox backend implementation.
//
// Files in this package are specific to Incus (the open-source LXC-based
// container manager). Cross-backend code lives in the parent pkg/sandbox
// package, which in personal/studio mode currently still relies on Incus
// being the only backend.
//
// This package must NOT import pkg/sandbox (that would create a cycle).
// Shared types that both packages need (e.g. TemplateRegistry) live in
// pkg/sandbox/tmplmeta, a leaf sub-package.
package incus
