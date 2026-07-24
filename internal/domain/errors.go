package domain

import "errors"

var (
	ErrNotFound             = errors.New("对象不存在")
	ErrConflict             = errors.New("并发版本冲突")
	ErrInvalidTransition    = errors.New("非法状态迁移")
	ErrEvidenceInsufficient = errors.New("缺少状态迁移所需证据")
	ErrUnauthorized         = errors.New("无权执行该操作")
	ErrPolicyDenied         = errors.New("策略拒绝该操作")
	ErrAlreadyExists        = errors.New("对象已经存在")
)
