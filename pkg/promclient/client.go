package promclient

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type Client struct {
	api promv1.API
}

func NewClient(baseURL string) (*Client, error) {
	c, err := api.NewClient(api.Config{Address: baseURL})
	if err != nil {
		return nil, fmt.Errorf("prometheus api client: %w", err)
	}
	return &Client{api: promv1.NewAPI(c)}, nil
}

// QueryInstant runs an instant query. If no series or empty result, ok is false and err is nil.
func (c *Client) QueryInstant(ctx context.Context, query string) (v float64, ok bool, err error) {
	val, warnings, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		return 0, false, fmt.Errorf("prometheus query: %w", err)
	}
	_ = warnings // intentionally ignored for now

	switch v := val.(type) {
	case model.Vector:
		if len(v) == 0 {
			return 0, false, nil
		}
		f := float64(v[0].Value)
		return f, true, nil
	case *model.Scalar:
		return float64(v.Value), true, nil
	default:
		// Recording rules should produce a scalar or a single-series vector for our label selectors.
		return 0, false, fmt.Errorf("prometheus query: unexpected value type %T", val)
	}
}

// NewClientWithAPI is test-friendly constructor.
func NewClientWithAPI(api promv1.API) *Client {
	return &Client{api: api}
}
