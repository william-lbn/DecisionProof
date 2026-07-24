package ui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedWorkbench(t *testing.T) {
	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/app/", nil))
	if rr.Code != 200 {
		t.Fatalf("工作台首页状态码错误: %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "DecisionProof") || !strings.Contains(rr.Body.String(), "产品决策") {
		t.Fatal("工作台首页缺少产品标识")
	}
}
