package deploy

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const pollInterval = 3 * time.Second

// PollUntilReady polls strategy.WaitReady every 3s until success, timeout, or a terminal error.
// A zero timeout means no timeout beyond whatever deadline is already on ctx.
func PollUntilReady(
	ctx context.Context,
	strategy DeployStrategy,
	cl client.Client,
	dyn dynamic.Interface,
	namespace, name string,
	timeout time.Duration,
) (endpoint string, err error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	s := newSpinner()
	if s != nil {
		s.Start()
		defer s.Stop()
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		endpoint, err = strategy.WaitReady(ctx, cl, dyn, namespace, name)
		if err == nil {
			return endpoint, nil
		}
		// Surface terminal errors (e.g. scheduling failures) immediately.
		if isTerminalError(err) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for workload to become ready: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// isTerminalError returns true for errors that will never self-resolve,
// such as pod scheduling failures due to missing node labels.
func isTerminalError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "pod scheduling failed") || strings.Contains(msg, "FailedScheduling")
}

func newSpinner() *spinner.Spinner {
	fi, err := os.Stdout.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return nil
	}
	s := spinner.New(spinner.CharSets[14], 120*time.Millisecond)
	s.Writer = os.Stderr
	s.Suffix = " Waiting for workload to become ready..."
	return s
}

// PrintDeploySuccess writes endpoint, cost/hr, and follow-up guidance to stdout.
func PrintDeploySuccess(endpoint, costPerHour string) {
	fmt.Printf("\nReady\n")
	fmt.Printf("  Endpoint: %s\n", endpoint)
	fmt.Printf("  Cost:     %s\n", costPerHour)
	fmt.Printf("\nRun: fisher cost --watch for live cost updates\n")
}
