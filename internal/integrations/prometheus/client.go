package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	BaseURL     string
	BearerToken string
	HTTP        *http.Client
}
type QueryResult struct {
	Value     float64
	Timestamp float64
	NoData    bool
	Warnings  []string
}

func (c *Client) Query(ctx context.Context, query string, at time.Time) (QueryResult, error) {
	if c.BaseURL == "" || query == "" {
		return QueryResult{}, errors.New("Prometheus BaseURL 和 query 不能为空")
	}
	q := url.Values{"query": {query}}
	if !at.IsZero() {
		q.Set("time", strconv.FormatInt(at.Unix(), 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/query?"+q.Encode(), nil)
	if err != nil {
		return QueryResult{}, err
	}
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return QueryResult{}, fmt.Errorf("查询 Prometheus 失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return QueryResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return QueryResult{}, fmt.Errorf("Prometheus 返回 HTTP %d: %s", resp.StatusCode, string(data))
	}
	var payload struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Error    string   `json:"error"`
		Data     struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return QueryResult{}, err
	}
	if payload.Status != "success" {
		return QueryResult{}, fmt.Errorf("Prometheus 查询失败: %s", payload.Error)
	}
	if len(payload.Data.Result) == 0 {
		return QueryResult{NoData: true, Warnings: payload.Warnings}, nil
	}
	if len(payload.Data.Result) != 1 || len(payload.Data.Result[0].Value) != 2 {
		return QueryResult{}, errors.New("Prometheus 查询必须返回单一标量或单一向量结果")
	}
	var ts float64
	if err := json.Unmarshal(payload.Data.Result[0].Value[0], &ts); err != nil {
		return QueryResult{}, err
	}
	var valueStr string
	if err := json.Unmarshal(payload.Data.Result[0].Value[1], &valueStr); err != nil {
		return QueryResult{}, err
	}
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return QueryResult{}, err
	}
	return QueryResult{Value: value, Timestamp: ts, Warnings: payload.Warnings}, nil
}
