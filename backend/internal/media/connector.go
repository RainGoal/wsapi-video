// Package media implements the platform's sole, fixed-origin Sub2API adapter.
package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("media gateway is not configured")
var ErrOutputOriginDenied = errors.New("untrusted gateway image output origin")

// UnknownSubmission must never be retried as a new upstream create request.
type UnknownSubmission struct{}

func (*UnknownSubmission) Error() string {
	return "gateway submission outcome is unknown; operator review is required"
}

type GatewayError struct{ Status int }

func (e *GatewayError) Error() string { return fmt.Sprintf("gateway request failed (%d)", e.Status) }

type Config struct {
	Origin string
	// Keys is a versioned secret registry. Keep old entries while their tasks exist.
	Keys                             map[string]string
	ImageCredential, VideoCredential string
	OutputOrigins                    []string
	AllowHTTP                        bool // Local integration fixtures only; production must use HTTPS.
	MaxMediaBytes                    int64
}

type Connector struct {
	cfg     Config
	origin  *url.URL
	client  *http.Client
	outputs map[string]bool
}

func NewConnector(cfg Config) (*Connector, error) {
	if cfg.Origin == "" {
		return nil, ErrUnavailable
	}
	u, err := url.Parse(cfg.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(cfg.AllowHTTP && u.Scheme == "http")) || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("Sub2API origin must be a fixed HTTPS origin")
	}
	if cfg.MaxMediaBytes <= 0 {
		cfg.MaxMediaBytes = 256 << 20
	}
	keys := make(map[string]string, len(cfg.Keys))
	for ref, key := range cfg.Keys {
		keys[ref] = key
	}
	cfg.Keys = keys
	c := &Connector{cfg: cfg, origin: u, outputs: map[string]bool{}, client: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	c.outputs[u.Scheme+"://"+u.Host] = true
	for _, origin := range cfg.OutputOrigins {
		v, e := url.Parse(origin)
		if e != nil || v.Host == "" || v.User != nil || v.RawQuery != "" || v.Fragment != "" || (v.Scheme != "https" && !(cfg.AllowHTTP && v.Scheme == "http")) || (v.Path != "" && v.Path != "/") {
			return nil, errors.New("invalid output origin")
		}
		c.outputs[v.Scheme+"://"+v.Host] = true
	}
	return c, nil
}

func (c *Connector) Credential(kind string) (string, error) {
	if c == nil {
		return "", ErrUnavailable
	}
	ref := c.cfg.ImageCredential
	if kind == "video" {
		ref = c.cfg.VideoCredential
	}
	if ref == "" || c.cfg.Keys[ref] == "" {
		return "", ErrUnavailable
	}
	return ref, nil
}

// Input contains only protocol fields approved by a frozen platform product.
// References are resolved by the platform asset service, never supplied as URLs by clients.
type Input struct {
	Kind        string   `json:"kind"`
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	Duration    int      `json:"duration,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	AspectRatio string   `json:"aspect_ratio,omitempty"`
	Size        string   `json:"size,omitempty"`
	Quality     string   `json:"quality,omitempty"`
	Audio       *bool    `json:"audio,omitempty"`
	Images      []string `json:"images,omitempty"`
	Videos      []string `json:"videos,omitempty"`
	Audios      []string `json:"audios,omitempty"`
	StartFrame  string   `json:"start_frame_url,omitempty"`
	EndFrame    string   `json:"end_frame_url,omitempty"`
}

type Output struct {
	URL    string `json:"url,omitempty"`
	Base64 string `json:"b64_json,omitempty"`
}
type Result struct {
	ID      string
	Model   string
	State   string
	Outputs []Output
}

func (c *Connector) Submit(ctx context.Context, ref string, in Input) (Result, error) {
	if c == nil {
		return Result{}, ErrUnavailable
	}
	// The fixed public video contract uses resolution. Image-only fields must
	// never be silently dropped, including in tasks accepted by older versions.
	if in.Kind == "video" && (in.Size != "" || in.Quality != "") {
		return Result{}, &GatewayError{Status: http.StatusUnprocessableEntity}
	}
	payload := map[string]any{"model": in.Model, "prompt": in.Prompt}
	path := "/v1/images/generations/async"
	if in.Kind == "video" {
		path = "/v1/videos/generations"
		if in.Duration > 0 {
			payload["duration"] = in.Duration
		}
		if in.Resolution != "" {
			payload["resolution"] = in.Resolution
		}
		if in.AspectRatio != "" {
			payload["aspect_ratio"] = in.AspectRatio
		}
		if in.Audio != nil {
			payload["audio"] = *in.Audio
		}
		if len(in.Images) > 0 {
			payload["referenceImages"] = in.Images
		}
		if len(in.Videos) > 0 {
			payload["referenceVideos"] = in.Videos
		}
		if len(in.Audios) > 0 {
			payload["referenceAudios"] = in.Audios
		}
		if in.StartFrame != "" {
			payload["start_frame_url"] = in.StartFrame
		}
		if in.EndFrame != "" {
			payload["end_frame_url"] = in.EndFrame
		}
	} else if in.Kind == "image" {
		payload["n"] = 1
		if in.AspectRatio != "" {
			payload["aspect_ratio"] = in.AspectRatio
		}
		if in.Size != "" {
			payload["size"] = in.Size
		}
		if in.Quality != "" {
			payload["quality"] = in.Quality
		}
		if len(in.Images) > 0 {
			path = "/v1/images/edits/async"
			refs := make([]map[string]string, 0, len(in.Images))
			for _, u := range in.Images {
				refs = append(refs, map[string]string{"image_url": u})
			}
			payload["images"] = refs
		}
	} else {
		return Result{}, errors.New("unsupported media kind")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	resp, err := c.request(ctx, http.MethodPost, path, ref, bytes.NewReader(body))
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return Result{}, err
		}
		return Result{}, &UnknownSubmission{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode == 409 {
			return Result{}, &UnknownSubmission{}
		}
		return Result{}, &GatewayError{resp.StatusCode}
	}
	var raw struct {
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&raw) != nil {
		return Result{}, &UnknownSubmission{}
	}
	if raw.ID == "" {
		raw.ID = raw.TaskID
	}
	if !validTaskID(raw.ID) {
		return Result{}, &UnknownSubmission{}
	}
	return Result{ID: raw.ID, State: "queued"}, nil
}

func validTaskID(id string) bool {
	return id != "" && len(id) <= 512 && !strings.ContainsAny(id, "/\\?#%\r\n\x00") && id != "." && id != ".."
}

func (c *Connector) Poll(ctx context.Context, ref, kind, id string) (Result, error) {
	if !validTaskID(id) {
		return Result{}, errors.New("invalid gateway task identifier")
	}
	path := "/v1/images/tasks/" + url.PathEscape(id)
	if kind == "video" {
		path = "/v1/videos/" + url.PathEscape(id)
	}
	resp, err := c.request(ctx, http.MethodGet, path, ref, nil)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Result{}, &GatewayError{resp.StatusCode}
	}
	var raw struct {
		ID       string `json:"id"`
		TaskID   string `json:"task_id"`
		Model    string `json:"model"`
		Status   string `json:"status"`
		ImageURL string `json:"image_url"`
		Result   struct {
			Data []Output `json:"data"`
		} `json:"result"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&raw) != nil {
		return Result{}, errors.New("invalid gateway task response")
	}
	if raw.ID == "" {
		raw.ID = raw.TaskID
	}
	if raw.ID != id {
		return Result{}, errors.New("gateway returned a different task identity")
	}
	r := Result{ID: id, Model: raw.Model}
	switch raw.Status {
	case "queued":
		r.State = "queued"
	case "processing", "in_progress":
		r.State = "generating"
	case "completed":
		r.State = "completed"
	case "failed":
		r.State = "failed"
	case "canceled", "cancelled":
		r.State = "canceled"
	default:
		return Result{}, errors.New("unrecognized gateway task status")
	}
	if kind == "image" && r.State == "completed" {
		r.Outputs = raw.Result.Data
		if len(r.Outputs) == 0 && raw.ImageURL != "" {
			r.Outputs = []Output{{URL: raw.ImageURL}}
		}
		if len(r.Outputs) != 1 {
			return Result{}, errors.New("gateway returned an unexpected image count")
		}
	}
	return r, nil
}

func (c *Connector) Cancel(ctx context.Context, ref, id string) error {
	if !validTaskID(id) {
		return errors.New("invalid gateway task identifier")
	}
	resp, err := c.request(ctx, http.MethodDelete, "/v1/videos/jobs/"+url.PathEscape(id), ref, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &GatewayError{resp.StatusCode}
	}
	return nil
}

func (c *Connector) OpenVideo(ctx context.Context, ref, id string) (io.ReadCloser, string, error) {
	if !validTaskID(id) {
		return nil, "", errors.New("invalid gateway task identifier")
	}
	resp, err := c.request(ctx, http.MethodGet, "/v1/videos/"+url.PathEscape(id)+"/content", ref, nil)
	return c.mediaResponse(resp, err)
}

func (c *Connector) OpenImage(ctx context.Context, out Output) (io.ReadCloser, string, error) {
	if c == nil {
		return nil, "", ErrUnavailable
	}
	if out.Base64 != "" {
		if int64(base64.StdEncoding.DecodedLen(len(out.Base64))) > c.cfg.MaxMediaBytes {
			return nil, "", errors.New("image exceeds archive limit")
		}
		b, err := base64.StdEncoding.DecodeString(out.Base64)
		if err != nil {
			return nil, "", errors.New("invalid image encoding")
		}
		return io.NopCloser(bytes.NewReader(b)), http.DetectContentType(b), nil
	}
	u, err := url.Parse(out.URL)
	if err != nil || u.User != nil || u.Fragment != "" {
		return nil, "", errors.New("invalid image output URL")
	}
	if !c.outputs[u.Scheme+"://"+u.Host] {
		return nil, "", ErrOutputOriginDenied
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", errors.New("invalid image output URL")
	}
	// Object-storage URLs never receive the gateway credential.
	resp, err := c.client.Do(req)
	return c.mediaResponse(resp, err)
}

func (c *Connector) mediaResponse(resp *http.Response, err error) (io.ReadCloser, string, error) {
	if err != nil {
		return nil, "", errors.New("gateway media download failed")
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, "", &GatewayError{resp.StatusCode}
	}
	if resp.ContentLength > c.cfg.MaxMediaBytes {
		resp.Body.Close()
		return nil, "", errors.New("media exceeds archive limit")
	}
	return &boundedBody{ReadCloser: resp.Body, remaining: c.cfg.MaxMediaBytes}, resp.Header.Get("Content-Type"), nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining < 0 {
		return 0, errors.New("media exceeds archive limit")
	}
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, e := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		return 0, errors.New("media exceeds archive limit")
	}
	return n, e
}

func (c *Connector) request(ctx context.Context, method, path, ref string, body io.Reader) (*http.Response, error) {
	if c == nil || c.cfg.Keys[ref] == "" {
		return nil, ErrUnavailable
	}
	u := *c.origin
	u.Path = path
	u.RawPath = ""
	u.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, errors.New("cannot build gateway request")
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Keys[ref])
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.New("gateway transport error")
	}
	return resp, nil
}
