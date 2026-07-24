package store

import "context"

type tenantContextKey struct{}

// WithTenant 把已认证租户写入数据库事务上下文。
// 该值只能由服务层根据可信身份设置，禁止从未经验证的请求字段直接注入。
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

func tenantFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(tenantContextKey{}).(string)
	return value, ok && value != ""
}
