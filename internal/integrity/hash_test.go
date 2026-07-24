package integrity

import "testing"

func TestCanonicalDigestIsStable(t *testing.T) {
	a := map[string]any{"b": 2, "a": 1}
	b := map[string]any{"a": 1, "b": 2}
	da, err := SHA256Object(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := SHA256Object(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Fatalf("相同结构化对象摘要不稳定: %s != %s", da, db)
	}
	if SHA256Bytes([]byte("a")) == SHA256Bytes([]byte("b")) {
		t.Fatal("不同内容不应产生相同测试摘要")
	}
	if _, err := CanonicalJSON(func() {}); err == nil {
		t.Fatal("不可序列化对象必须失败")
	}
}
