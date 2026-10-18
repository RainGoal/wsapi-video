package settings

import (
	"context"
	"io"
	"time"
	"wsapi-video/internal/media"
)

// GatewayRouter routes every existing job to the immutable credential version
// captured at acceptance. Disabling new generation never strands old jobs.
type GatewayRouter struct{ Settings *Service }

func (g *GatewayRouter) Credential(kind string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, e := g.Settings.Gateway(ctx, "")
	if e != nil {
		return "", e
	}
	return c.Credential(kind)
}
func (g *GatewayRouter) Submit(ctx context.Context, ref string, in media.Input) (media.Result, error) {
	c, e := g.Settings.Gateway(ctx, ref)
	if e != nil {
		return media.Result{}, e
	}
	return c.Submit(ctx, ref, in)
}
func (g *GatewayRouter) Poll(ctx context.Context, ref, kind, id string) (media.Result, error) {
	c, e := g.Settings.Gateway(ctx, ref)
	if e != nil {
		return media.Result{}, e
	}
	return c.Poll(ctx, ref, kind, id)
}
func (g *GatewayRouter) Cancel(ctx context.Context, ref, id string) error {
	c, e := g.Settings.Gateway(ctx, ref)
	if e != nil {
		return e
	}
	return c.Cancel(ctx, ref, id)
}
func (g *GatewayRouter) OpenVideo(ctx context.Context, ref, id string) (io.ReadCloser, string, error) {
	c, e := g.Settings.Gateway(ctx, ref)
	if e != nil {
		return nil, "", e
	}
	return c.OpenVideo(ctx, ref, id)
}
func (g *GatewayRouter) OpenImage(ctx context.Context, out media.Output) (io.ReadCloser, string, error) {
	c, e := g.Settings.Gateway(ctx, "")
	if e != nil {
		return nil, "", e
	}
	return c.OpenImage(ctx, out)
}
