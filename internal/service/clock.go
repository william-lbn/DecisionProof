package service

import "time"

// Clock 允许测试固定时间，同时避免业务代码直接散落 time.Now 调用。
type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }
