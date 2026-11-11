// Package admin 管理唯一管理员的双因素认证、首次初始化和可撤销会话。
package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"

	"epay/internal/securebox"
)

const (
	sessionLifetime = 12 * time.Hour
	totpAAD         = "admin:totp:1"
)

var (
	ErrAlreadyInitialized = errors.New("administrator is already initialized")
	ErrCredentials        = errors.New("invalid administrator credentials")
	ErrRateLimited        = errors.New("login temporarily unavailable")
)

// RateLimitError 携带本次拒绝后建议等待的时长，兼容 errors.Is(ErrRateLimited)。
type RateLimitError struct {
	RetryAfter time.Duration
}

// Error 返回登录请求受限的错误说明。
func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }

// Unwrap 返回 ErrRateLimited，让调用方可以用 errors.Is 判断是否被限流。
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// Repository 限定身份认证所需的持久化及原子提交能力。
type Repository interface {
	// Credentials 读取管理员的密码哈希、加密绑定密钥和已用动态码周期，并返回管理员是否存在。
	Credentials(context.Context) (Credentials, bool, error)
	// CreateAdministrator 创建唯一管理员并记录初始化事件；已存在时返回 false。
	CreateAdministrator(context.Context, Credentials, time.Time) (bool, error)
	// ResetTOTP 仅在密码和旧绑定未被修改时更换绑定，同时删除旧会话并记录重置事件。
	ResetTOTP(context.Context, Credentials, []byte, int64, time.Time) (bool, error)
	// ReserveLoginAttempt 记录登录尝试，并返回是否允许继续及受限时还需等待的时长。
	ReserveLoginAttempt(context.Context, string, time.Time) (bool, time.Duration, error)
	// CommitLogin 记录动态码已使用并创建会话；密码或绑定已变化、动态码周期已用时拒绝创建。
	CommitLogin(context.Context, Credentials, int64, []byte, []byte, time.Time, time.Time, string) (bool, error)
	// SessionCSRFHash 读取有效会话的 CSRF 令牌哈希；会话不存在或已过期时返回未找到。
	SessionCSRFHash(context.Context, []byte, time.Time) ([]byte, bool, error)
	// DeleteSession 删除指定会话并记录退出事件，使旧登录令牌失效。
	DeleteSession(context.Context, []byte) error
}

// Session 携带一次生成的浏览器凭证，数据库只保存其摘要。
type Session struct {
	Token, CSRFToken string
	ExpiresAt        time.Time
}

// Service 负责管理员身份认证与会话管理，并限制密码派生的并发数以控制资源消耗。
type Service struct {
	repository Repository
	box        *securebox.Box
	loginSlots chan struct{} // 带缓冲的 channel 实现兵法限制
}

// New 创建管理员服务，使用传入的仓储保存数据、加密器保护 TOTP 绑定密钥。
func New(repository Repository, box *securebox.Box) (*Service, error) {
	if repository == nil || box == nil {
		return nil, errors.New("administrator repository and encryption box are required")
	}
	return &Service{repository: repository, box: box, loginSlots: make(chan struct{}, 2)}, nil
}

// Initialized 检查管理员是否已经创建，用于决定是否显示初始化页面。
func (s *Service) Initialized(ctx context.Context) (bool, error) {
	_, exists, err := s.repository.Credentials(ctx)
	return exists, err
}

// Initialize 检查密码格式和新验证器的动态码，然后创建唯一管理员。
// 管理员已存在时拒绝覆盖；创建时会记录本次动态码已使用。
func (s *Service) Initialize(ctx context.Context, password, secret, code string) error {
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		return &RateLimitError{RetryAfter: time.Second}
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	exists, err := s.Initialized(ctx)
	if err != nil {
		return err
	}
	if exists {
		return ErrAlreadyInitialized
	}
	now := time.Now().UTC()
	step, err := MatchTOTP(secret, code, now)
	if err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	encrypted, err := s.box.Seal(totpAAD, []byte(secret))
	if err != nil {
		return err
	}
	created, err := s.repository.CreateAdministrator(ctx, Credentials{hash, encrypted, step}, now)
	if err != nil {
		return err
	}
	if !created {
		return ErrAlreadyInitialized
	}
	return nil
}

// VerifyRecoveryPassword 检查管理员密码是否正确，仅供本地终端更换验证器前使用。
func (s *Service) VerifyRecoveryPassword(ctx context.Context, password string) error {
	c, exists, err := s.repository.Credentials(ctx)
	if err != nil {
		return err
	}
	if !exists || !verifyPassword(c.PasswordHash, password) {
		return ErrCredentials
	}
	return nil
}

// ResetTOTP 检查管理员密码和新验证器的动态码，然后保存新绑定并使旧登录会话失效。
func (s *Service) ResetTOTP(ctx context.Context, password, secret, code string) error {
	c, exists, err := s.repository.Credentials(ctx)
	if err != nil {
		return err
	}
	if !exists || !verifyPassword(c.PasswordHash, password) {
		return ErrCredentials
	}
	now := time.Now().UTC()
	step, err := MatchTOTP(secret, code, now)
	if err != nil {
		return err
	}
	encrypted, err := s.box.Seal(totpAAD, []byte(secret))
	if err != nil {
		return err
	}
	changed, err := s.repository.ResetTOTP(ctx, c, encrypted, step, now)
	if err != nil {
		return err
	}
	if !changed {
		return ErrCredentials
	}
	return nil
}

// Login 检查密码和动态码，全部通过后才创建登录会话；同一动态码周期只能使用一次。
func (s *Service) Login(ctx context.Context, remote, password, code string) (Session, error) {
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		return Session{}, &RateLimitError{RetryAfter: time.Second}
	}
	if remote == "" {
		return Session{}, ErrCredentials
	}
	remote = "peer:" + remote
	now := time.Now().UTC()
	allowed, retryAfter, err := s.repository.ReserveLoginAttempt(ctx, remote, now)
	if err != nil {
		return Session{}, err
	}
	if !allowed {
		return Session{}, &RateLimitError{RetryAfter: retryAfter}
	}
	c, exists, err := s.repository.Credentials(ctx)
	if err != nil {
		return Session{}, err
	}
	if !exists || !verifyPassword(c.PasswordHash, password) || len(c.TOTPCiphertext) == 0 {
		return Session{}, ErrCredentials
	}
	secret, err := s.box.Open(totpAAD, c.TOTPCiphertext)
	if err != nil {
		return Session{}, err
	}
	step, err := MatchTOTP(string(secret), code, now)
	if err != nil || step <= c.LastTOTPStep {
		return Session{}, ErrCredentials
	}
	token, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	expires := now.Add(sessionLifetime)
	committed, err := s.repository.CommitLogin(ctx, c, step, tokenHash(token), tokenHash(csrf), now, expires, remote)
	if err != nil {
		return Session{}, err
	}
	if !committed {
		return Session{}, ErrCredentials
	}
	return Session{token, csrf, expires}, nil
}

// ValidSession 检查登录令牌格式是否正确，以及对应会话是否存在且未过期。
func (s *Service) ValidSession(ctx context.Context, token string) (bool, error) {
	if !validToken(token) {
		return false, nil
	}
	_, exists, err := s.repository.SessionCSRFHash(ctx, tokenHash(token), time.Now().UTC())
	return exists, err
}

// ValidCSRF 检查表单令牌是否属于当前登录会话；会话不存在或已过期时也会拒绝。
func (s *Service) ValidCSRF(ctx context.Context, token, csrf string) (bool, error) {
	if !validToken(token) || !validToken(csrf) {
		return false, nil
	}
	expected, exists, err := s.repository.SessionCSRFHash(ctx, tokenHash(token), time.Now().UTC())
	if err != nil || !exists {
		return false, err
	}
	return subtle.ConstantTimeCompare(expected, tokenHash(csrf)) == 1, nil
}

// Logout 删除服务端的会话记录，使对应的登录 Cookie 失效。
func (s *Service) Logout(ctx context.Context, token string) error {
	if !validToken(token) {
		return nil
	}
	return s.repository.DeleteSession(ctx, tokenHash(token))
}

// randomToken 生成 32 字节随机令牌，用于登录会话或表单校验。
func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// tokenHash 计算令牌的 SHA-256 哈希，供数据库保存和比对，避免保存原始令牌。
func tokenHash(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

// validToken 检查令牌是否为无填充的 Base64 URL 编码，且解码后正好是 32 字节。
func validToken(value string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(raw) == 32 && len(value) == 43
}
