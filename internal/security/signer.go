package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Signer 对发布证据清单进行离线可验证签名。
// 示例使用 Ed25519；生产中私钥应由 KMS/HSM 或 Sigstore Keyless 身份管理，不应长期放在环境变量中。
type Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

// NewEphemeralSigner 生成临时密钥，仅用于测试和本地演示。
func NewEphemeralSigner() (*Signer, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("生成 Ed25519 密钥失败: %w", err)
	}
	return &Signer{privateKey: privateKey, publicKey: publicKey}, nil
}

// NewSignerFromSeedBase64 从 32 字节种子构造签名器。
func NewSignerFromSeedBase64(encoded string) (*Signer, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("解析签名种子失败: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("签名种子长度必须为 %d 字节", ed25519.SeedSize)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return &Signer{privateKey: privateKey, publicKey: publicKey}, nil
}

// NewSignerFromEnv 优先读取指定环境变量；不存在时根据 allowEphemeral 决定是否生成临时密钥。
func NewSignerFromEnv(envName string, allowEphemeral bool) (*Signer, error) {
	value := os.Getenv(envName)
	if value != "" {
		return NewSignerFromSeedBase64(value)
	}
	if !allowEphemeral {
		return nil, errors.New("生产模式禁止自动生成临时签名密钥")
	}
	return NewEphemeralSigner()
}

func (s *Signer) Sign(data []byte) []byte { return ed25519.Sign(s.privateKey, data) }

func (s *Signer) Verify(data, signature []byte) bool {
	return ed25519.Verify(s.publicKey, data, signature)
}

func (s *Signer) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(s.publicKey)
}

// PublicKeyID 返回公钥的稳定 SHA-256 标识，用于清单中的密钥版本追踪。
func (s *Signer) PublicKeyID() string {
	sum := sha256.Sum256(s.publicKey)
	return fmt.Sprintf("sha256:%x", sum[:])
}

// VerifyWithPublicKeyBase64 使用清单自带的公钥验证签名。
// 这只证明“清单未被持有对应私钥者之后篡改”；生产环境还必须把 SigningKeyID
// 与企业 KMS、Sigstore 身份或可信根列表进行匹配，不能无条件信任任意自带公钥。
func VerifyWithPublicKeyBase64(encoded string, data, signature []byte) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return fmt.Errorf("解析清单公钥失败: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return fmt.Errorf("清单公钥长度必须为 %d 字节", ed25519.PublicKeySize)
	}
	if !ed25519.Verify(ed25519.PublicKey(raw), data, signature) {
		return errors.New("发布清单签名验证失败")
	}
	return nil
}
