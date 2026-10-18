package flow_test

import (
	"encoding/json"
	"strings"
	"testing"

	"httpproxy/internal/flow"
)

func TestSummaryOmitsBodies(t *testing.T) {
	f := flow.Flow{
		ID:         "abc",
		Method:     "GET",
		Host:       "example.com",
		Path:       "/x",
		ReqBody:    []byte("secret-req"),
		RespBody:   []byte("secret-resp"),
		StatusCode: 200,
	}
	s := f.Summary()
	if s.ID != "abc" || s.Method != "GET" || s.StatusCode != 200 {
		t.Fatalf("unexpected summary meta: %+v", s)
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if strings.Contains(string(b), "secret-req") || strings.Contains(string(b), "secret-resp") {
		t.Fatal("bodies leaked into summary JSON")
	}
}
