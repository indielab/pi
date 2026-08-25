module pidiff

go 1.26

require github.com/sky-valley/pi v0.0.0

// difftest is its OWN module, and that is load-bearing rather than incidental:
// a subdirectory containing a go.mod is excluded from the parent module, so the
// published, public, MIT github.com/sky-valley/pi ships not one byte of this
// harness. It stays in the repo for lockstep versioning with the code it
// verifies, and out of the artefact consumers download.
//
// Relative so the harness works in any clone: difftest -> repo root.
// The two sides live in pi/ (real pi) and port/ (this port); the module root is
// difftest/ itself, so `go build ./...` here cannot collide with a dir named go.
replace github.com/sky-valley/pi => ..
