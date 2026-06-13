// Package integration runs the full suite of API integration tests against every
// registered storage backend. Each backend implements the Backend interface,
// which knows how to set up/tear down its stores and reset data between tests.
//
// To add a new backend:
//  1. Create a file like backend_mydb_test.go that returns a Backend.
//  2. Register it in TestMain via registerBackend("mydb", newMyDBBackend).
//
// All test logic is in backend-agnostic spec files (pages_test.go, etc.).
// They call RunSpecs(t) which runs them once per registered backend.
package integration
