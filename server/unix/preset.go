//go:build unix

package unix

import (
	"github.com/sky-valley/pi/server"
)

// NewServer composes a server.Server with exactly one Unix-domain listener. It
// is the assembly nearly every caller wants; use NewListener with server.New
// directly to serve on several transports at once.
func NewServer(backend server.Backend, options ServerOptions) (*server.Server, error) {
	listener, err := NewListener(options.listenerOptions())
	if err != nil {
		return nil, err
	}
	return server.New(backend, options.serverOptions(listener))
}
