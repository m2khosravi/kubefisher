package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// prometheusQueryTestServer returns an instant-query API compatible with client_golang's prom API.
// It routes on the `query` parameter: token queries must reference vLLM generation token counters;
// cost queries must contain "cost_per_hour".
func prometheusQueryTestServer(tokenVal float64, costVal float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query().Get("query")
		if q == "" && r.Method == http.MethodPost {
			_ = r.ParseForm()
			q = r.FormValue("query")
		}
		var v float64
		switch {
		case strings.Contains(q, "vllm:generation_tokens_total") || strings.Contains(q, "vllm:num_generation_tokens_total"):
			v = tokenVal
		case strings.Contains(q, "cost_per_hour"):
			v = costVal
		default:
			writePrometheusVector(w, 0)
			return
		}
		writePrometheusVector(w, v)
	}))
}

func writePrometheusVector(w http.ResponseWriter, v float64) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "vector",
			"result": []map[string]any{
				{
					"metric": map[string]string{},
					"value":  []any{float64(1), fmt.Sprintf("%g", v)},
				},
			},
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
