package prometheus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"value":[1710000000,"0.12"]}]}}`))
	}))
	defer srv.Close()
	result, err := (&Client{BaseURL: srv.URL}).Query(context.Background(), "up", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value != 0.12 || result.NoData {
		t.Fatalf("bad result %+v", result)
	}
}
