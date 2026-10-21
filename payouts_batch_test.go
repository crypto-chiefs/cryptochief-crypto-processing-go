package cryptochief

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPayoutsBatch_HTTPStatusDecode(t *testing.T) {
	cases := []struct {
		name           string
		path           string
		responsePrefix string
		successItem    string
	}{
		{"estimate", "/v1/payout/batch/estimate", `{"total":6,"ok":1,"failed":5,"total_estimated_fee_fiat":"0.10","items":[`, `{"index":2,"status":"ok","estimate":{"amount":"1"}}`},
		{"execute", "/v1/payout/batch/execute", `{"total":6,"accepted":1,"rejected":5,"items":[`, `{"index":2,"order_id":"o2","status":"queue","uuid":"p2","payout":{"uuid":"p2"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var path, body string
			srv := captureServer(t, tc.responsePrefix+`
				{"index":0,"order_id":"o0","status":"error","error":"FAILED_TO_ESTIMATE_FEE","http_status":400},
				{"index":1,"order_id":"o1","status":"error","error":"FAILED_TO_ESTIMATE_FEE","http_status":502},
				`+tc.successItem+`,
				{"index":3,"order_id":"o3","status":"error","error":"FUTURE_ERROR","http_status":599},
				{"index":4,"order_id":"o4","status":"error","error":"LEGACY_ERROR"},
				{"index":5,"order_id":"o5","status":"error","error":"NULL_STATUS","http_status":null}
			]}`, &path, &body)
			c, err := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
			if err != nil {
				t.Fatal(err)
			}
			var out *BatchExecuteResponse
			if tc.name == "estimate" {
				out, err = c.Payouts.BatchEstimate(context.Background(), &BatchExecuteRequest{})
			} else {
				out, err = c.Payouts.BatchExecute(context.Background(), &BatchExecuteRequest{})
			}
			if err != nil {
				t.Fatalf("batch %s: %v", tc.name, err)
			}
			if path != tc.path {
				t.Errorf("path = %q, want %q", path, tc.path)
			}
			if len(out.Items) != 6 {
				t.Fatalf("items = %d, want 6", len(out.Items))
			}
			for i, want := range []int{400, 502, 0, 599, 0, 0} {
				item := out.Items[i]
				if item.Index != i {
					t.Errorf("items[%d].index = %d", i, item.Index)
				}
				if want == 0 {
					if item.HTTPStatus != nil {
						t.Errorf("items[%d].http_status = %d, want absent", i, *item.HTTPStatus)
					}
				} else if item.HTTPStatus == nil || *item.HTTPStatus != want {
					t.Errorf("items[%d].http_status = %v, want %d", i, derefInt(item.HTTPStatus), want)
				}
				if got := item.IsRetryable(); got != (want == 502) {
					t.Errorf("items[%d].IsRetryable() = %v, want %v", i, got, want == 502)
				}
			}
			if out.Items[0].Error != "FAILED_TO_ESTIMATE_FEE" || out.Items[1].Error != "FAILED_TO_ESTIMATE_FEE" {
				t.Errorf("error codes = %q/%q", out.Items[0].Error, out.Items[1].Error)
			}
			if tc.name == "estimate" && out.Items[2].Status != "ok" {
				t.Errorf("successful estimate status = %q, want ok", out.Items[2].Status)
			}
			if tc.name == "execute" && (out.Items[2].Status != PayoutStatusQueue || out.Items[2].UUID != "p2") {
				t.Errorf("successful execute = %+v", out.Items[2])
			}
		})
	}
}

func TestBatchItemResult_HTTPStatusMarshal(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"success ok", `{"status":"ok"}`, ""},
		{"success queue", `{"status":"queue"}`, ""},
		{"legacy error", `{"status":"error","error":"LEGACY_ERROR"}`, ""},
		{"null", `{"status":"error","http_status":null}`, ""},
		{"zero", `{"status":"error","http_status":0}`, "0"},
		{"400", `{"status":"error","http_status":400}`, "400"},
		{"502", `{"status":"error","http_status":502}`, "502"},
		{"unknown", `{"status":"error","http_status":599}`, "599"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var item BatchItemResult
			if err := json.Unmarshal([]byte(tc.json), &item); err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(b, &fields); err != nil {
				t.Fatal(err)
			}
			if got := string(fields["http_status"]); got != tc.want {
				t.Errorf("http_status = %q, want %q; JSON = %s", got, tc.want, b)
			}
		})
	}
}

func TestBatchItemResult_IsRetryable(t *testing.T) {
	cases := []struct {
		name       string
		httpStatus int
		want       bool
	}{
		{"absent", 0, false},
		{"zero", 0, false},
		{"400", 400, false},
		{"408", 408, false},
		{"500", 500, false},
		{"502", 502, true},
		{"503", 503, false},
		{"504", 504, false},
		{"unknown", 599, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := BatchItemResult{Status: "error", Error: "FAILED_TO_ESTIMATE_FEE"}
			if tc.name != "absent" {
				item.HTTPStatus = &tc.httpStatus
			}
			if got := item.IsRetryable(); got != tc.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tc.want)
			}
		})
	}
}
