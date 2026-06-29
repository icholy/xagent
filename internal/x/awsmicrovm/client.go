// Package awsmicrovm is a general-purpose Go client and lifecycle-hook server
// for the AWS Lambda MicroVMs service — what AWS would ship if they wrote a Go
// package for MicroVMs. It models the service and nothing about how any
// particular application uses it: no tasks, sandboxes, or staging semantics. The
// run-hook payload is passed through verbatim; its meaning is the caller's
// business.
//
// PREVIEW: AWS Lambda MicroVMs has no official Go SDK at the time of writing.
// The control-plane wire surface here (Lambda endpoint host, the /2025-09-09
// operation paths, HTTP methods, and JSON field names) is taken from the
// service's own API model (the lambda-microvms botocore service-2.json shipped
// with aws-cli) rather than guessed, but is verified in this package only
// against an httptest server. Credentials, region, and SigV4 signing (as the
// "lambda" service) use aws-sdk-go-v2; replace this with the official SDK when
// one ships.
package awsmicrovm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// signingName is the SigV4 service name. Lambda MicroVMs is part of the Lambda
// service: requests are signed as "lambda" and sent to the Lambda endpoint
// (lambda.<region>.amazonaws.com), under the date-stamped API version below.
const signingName = "lambda"

// apiVersion is the date-stamped path prefix for the MicroVMs operations, e.g.
// POST /2025-09-09/microvms.
const apiVersion = "2025-09-09"

// MicrovmState is the lifecycle state Lambda reports for a MicroVM. The values
// are the service's MicrovmState enum.
type MicrovmState string

const (
	MicrovmStatePending     MicrovmState = "PENDING"
	MicrovmStateRunning     MicrovmState = "RUNNING"
	MicrovmStateSuspending  MicrovmState = "SUSPENDING"
	MicrovmStateSuspended   MicrovmState = "SUSPENDED"
	MicrovmStateTerminating MicrovmState = "TERMINATING"
	MicrovmStateTerminated  MicrovmState = "TERMINATED"
)

// Terminal reports whether the state is an end state (the MicroVM is being
// torn down or is gone, and will not run again).
func (s MicrovmState) Terminal() bool {
	return s == MicrovmStateTerminating || s == MicrovmStateTerminated
}

// Microvm is a point-in-time view of a MicroVM. Tags is not populated by
// GetMicrovm/ListMicrovms (the service does not return tags on those); it is
// here for callers that resolve tags separately (ListTags) — note running
// MicroVM instances are not a taggable resource on the service.
type Microvm struct {
	MicrovmID string
	State     MicrovmState
	Endpoint  string
	Tags      map[string]string
}

// Alive reports whether the MicroVM is not terminal (pending, running,
// suspending, or suspended).
func (m Microvm) Alive() bool { return !m.State.Terminal() }

// IdlePolicy configures automatic suspend/resume on endpoint idleness.
type IdlePolicy struct {
	AutoResumeEnabled        bool  `json:"autoResumeEnabled"`
	MaxIdleDurationSeconds   int64 `json:"maxIdleDurationSeconds,omitempty"`
	SuspendedDurationSeconds int64 `json:"suspendedDurationSeconds,omitempty"`
}

// Client is a Lambda MicroVMs control-plane client.
type Client struct {
	region   string
	creds    aws.CredentialsProvider
	signer   *v4.Signer
	endpoint string
	http     *http.Client
	now      func() time.Time
}

// NewClient builds a control-plane client from an AWS config. The endpoint
// defaults to https://lambda.<region>.amazonaws.com (the Lambda service host).
func NewClient(cfg aws.Config) *Client {
	return &Client{
		region:   cfg.Region,
		creds:    cfg.Credentials,
		signer:   v4.NewSigner(),
		endpoint: fmt.Sprintf("https://lambda.%s.amazonaws.com", cfg.Region),
		http:     http.DefaultClient,
		now:      time.Now,
	}
}

// RunMicrovmInput is the input to RunMicrovm.
type RunMicrovmInput struct {
	ImageIdentifier          string      `json:"imageIdentifier"`
	ExecutionRoleArn         string      `json:"executionRoleArn,omitempty"`
	EgressNetworkConnectors  []string    `json:"egressNetworkConnectors,omitempty"`
	IngressNetworkConnectors []string    `json:"ingressNetworkConnectors,omitempty"`
	MaximumDurationInSeconds int64       `json:"maximumDurationInSeconds,omitempty"`
	RunHookPayload           string      `json:"runHookPayload,omitempty"`
	IdlePolicy               *IdlePolicy `json:"idlePolicy,omitempty"`
}

// RunMicrovmOutput is the result of RunMicrovm.
type RunMicrovmOutput struct {
	MicrovmID string `json:"microvmId"`
	Endpoint  string `json:"endpoint"`
}

// microvmsPath is the collection path; microvmPath is a single MicroVM's path.
func microvmsPath() string         { return "/" + apiVersion + "/microvms" }
func microvmPath(id string) string { return microvmsPath() + "/" + url.PathEscape(id) }

// RunMicrovm launches a MicroVM from an image. The MicroVM runs autonomously
// until suspended or terminated.
func (c *Client) RunMicrovm(ctx context.Context, in *RunMicrovmInput) (*RunMicrovmOutput, error) {
	var out RunMicrovmOutput
	if err := c.do(ctx, "RunMicrovm", http.MethodPost, microvmsPath(), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TerminateMicrovmInput is the input to TerminateMicrovm.
type TerminateMicrovmInput struct {
	MicrovmID string `json:"-"`
}

// TerminateMicrovmOutput is the result of TerminateMicrovm.
type TerminateMicrovmOutput struct{}

// TerminateMicrovm terminates a MicroVM, firing its /terminate lifecycle hook
// before releasing resources.
func (c *Client) TerminateMicrovm(ctx context.Context, in *TerminateMicrovmInput) (*TerminateMicrovmOutput, error) {
	if err := c.do(ctx, "TerminateMicrovm", http.MethodDelete, microvmPath(in.MicrovmID), nil, nil); err != nil {
		return nil, err
	}
	return &TerminateMicrovmOutput{}, nil
}

// SuspendMicrovmInput is the input to SuspendMicrovm.
type SuspendMicrovmInput struct {
	MicrovmID string `json:"-"`
}

// SuspendMicrovmOutput is the result of SuspendMicrovm.
type SuspendMicrovmOutput struct{}

// SuspendMicrovm suspends a running MicroVM, pausing it while preserving its
// state so it can later be resumed.
func (c *Client) SuspendMicrovm(ctx context.Context, in *SuspendMicrovmInput) (*SuspendMicrovmOutput, error) {
	if err := c.do(ctx, "SuspendMicrovm", http.MethodPost, microvmPath(in.MicrovmID)+"/suspend", nil, nil); err != nil {
		return nil, err
	}
	return &SuspendMicrovmOutput{}, nil
}

// ResumeMicrovmInput is the input to ResumeMicrovm.
type ResumeMicrovmInput struct {
	MicrovmID string `json:"-"`
}

// ResumeMicrovmOutput is the result of ResumeMicrovm.
type ResumeMicrovmOutput struct{}

// ResumeMicrovm resumes a previously suspended MicroVM, returning it to the
// running state.
func (c *Client) ResumeMicrovm(ctx context.Context, in *ResumeMicrovmInput) (*ResumeMicrovmOutput, error) {
	if err := c.do(ctx, "ResumeMicrovm", http.MethodPost, microvmPath(in.MicrovmID)+"/resume", nil, nil); err != nil {
		return nil, err
	}
	return &ResumeMicrovmOutput{}, nil
}

// GetMicrovmInput is the input to GetMicrovm.
type GetMicrovmInput struct {
	MicrovmID string `json:"-"`
}

// GetMicrovmOutput is the result of GetMicrovm.
type GetMicrovmOutput struct {
	Microvm Microvm
}

// wireMicrovm is the JSON shape of a MicroVM in control-plane responses. The
// service does not return tags here, so Microvm.Tags is left unset.
type wireMicrovm struct {
	MicrovmID string `json:"microvmId"`
	State     string `json:"state"`
	Endpoint  string `json:"endpoint"`
}

func (w wireMicrovm) toMicrovm() Microvm {
	return Microvm{MicrovmID: w.MicrovmID, State: MicrovmState(w.State), Endpoint: w.Endpoint}
}

// GetMicrovm returns a single MicroVM by id.
func (c *Client) GetMicrovm(ctx context.Context, in *GetMicrovmInput) (*GetMicrovmOutput, error) {
	var w wireMicrovm
	if err := c.do(ctx, "GetMicrovm", http.MethodGet, microvmPath(in.MicrovmID), nil, &w); err != nil {
		return nil, err
	}
	return &GetMicrovmOutput{Microvm: w.toMicrovm()}, nil
}

// ListMicrovmsInput is the input to ListMicrovms.
type ListMicrovmsInput struct {
	// ImageIdentifier optionally restricts the listing to MicroVMs of one image.
	ImageIdentifier string
	NextToken       string
}

// ListMicrovmsOutput is the result of ListMicrovms.
type ListMicrovmsOutput struct {
	Microvms  []Microvm
	NextToken string
}

type listMicrovmsResponse struct {
	Microvms  []wireMicrovm `json:"items"`
	NextToken string        `json:"nextToken"`
}

// ListMicrovms returns the MicroVMs visible to the caller. The listing carries
// only id/state/image per item — not the endpoint or tags; fetch those with
// GetMicrovm / ListTags.
func (c *Client) ListMicrovms(ctx context.Context, in *ListMicrovmsInput) (*ListMicrovmsOutput, error) {
	path := microvmsPath()
	q := url.Values{}
	if in != nil && in.ImageIdentifier != "" {
		q.Set("imageIdentifier", in.ImageIdentifier)
	}
	if in != nil && in.NextToken != "" {
		q.Set("nextToken", in.NextToken)
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp listMicrovmsResponse
	if err := c.do(ctx, "ListMicrovms", http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := &ListMicrovmsOutput{NextToken: resp.NextToken}
	for _, w := range resp.Microvms {
		out.Microvms = append(out.Microvms, w.toMicrovm())
	}
	return out, nil
}

// do signs and sends a JSON request to an operation path, decoding the response
// into out (which may be nil). Requests are signed with the AWS SDK's SigV4
// signer using credentials from the SDK credential chain.
func (c *Client) do(ctx context.Context, op, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = b
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	creds, err := c.creds.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("retrieve aws credentials: %w", err)
	}
	sum := sha256.Sum256(body)
	if err := c.signer.SignHTTP(ctx, creds, req, hex.EncodeToString(sum[:]), signingName, c.region, c.now().UTC()); err != nil {
		return fmt.Errorf("sign request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return newAPIError(op, resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
