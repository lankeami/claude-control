package tunnel

import (
	"context"
	"fmt"
	"net"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

type Tunnel struct {
	listener net.Listener
	url      string
}

func Start(ctx context.Context) (*Tunnel, error) {
	listener, err := ngrok.Listen(ctx, config.HTTPEndpoint())
	if err != nil {
		return nil, fmt.Errorf("ngrok listen: %w", err)
	}
	return &Tunnel{listener: listener, url: listener.Addr().String()}, nil
}

func (t *Tunnel) URL() string {
	return t.url
}

func (t *Tunnel) Listener() net.Listener {
	return t.listener
}

func (t *Tunnel) Close() error {
	return t.listener.Close()
}
