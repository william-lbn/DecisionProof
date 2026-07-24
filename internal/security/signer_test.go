package security

import (
	"encoding/base64"
	"testing"
)

func TestSignerPublicKeyIDAndDetachedVerification(t *testing.T) {
	signer, err := NewEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("decisionproof-manifest")
	signature := signer.Sign(data)
	if signer.PublicKeyID() == "" {
		t.Fatal("公钥标识不能为空")
	}
	if err := VerifyWithPublicKeyBase64(signer.PublicKeyBase64(), data, signature); err != nil {
		t.Fatalf("清单自带公钥应能验证签名: %v", err)
	}
	if err := VerifyWithPublicKeyBase64(signer.PublicKeyBase64(), []byte("tampered"), signature); err == nil {
		t.Fatal("修改内容后签名验证必须失败")
	}
	if err := VerifyWithPublicKeyBase64(base64.StdEncoding.EncodeToString([]byte("short")), data, signature); err == nil {
		t.Fatal("非法公钥长度必须失败")
	}
}
