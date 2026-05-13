// Package filestore provides the file-based implementation of the store interfaces.
// This wraps the existing Astonish file-based storage systems behind the abstract
// store.Store interfaces, enabling the same API handlers to work with either
// file-based (personal mode) or database-backed (platform mode) storage.
package filestore
