package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

// ---- mock db.UserQuery ----------------------------------------------------

type mockUserQuery struct {
	byEmail   map[string]*db.User
	byID      map[int64]*db.User
	exists    bool
	insertErr error
	updateErr error
	codeUser  *db.User // для GetByEmailAndCode
	codeErr   error
	verifyErr error
	beginErr  error
	inserted  *db.User
}

func newMockUserQuery() *mockUserQuery {
	return &mockUserQuery{byEmail: map[string]*db.User{}, byID: map[int64]*db.User{}, beginErr: errors.New("no tx in test")}
}

func (m *mockUserQuery) GetByID(ctx context.Context, id int64) (*db.User, error) {
	return m.byID[id], nil
}
func (m *mockUserQuery) GetByUsername(ctx context.Context, username string) (*db.User, error) {
	return nil, nil
}
func (m *mockUserQuery) GetByEmail(ctx context.Context, email string) (*db.User, error) {
	return m.byEmail[email], nil
}
func (m *mockUserQuery) ExistsByUsernameOrEmail(ctx context.Context, username, email string) (bool, error) {
	return m.exists, nil
}
func (m *mockUserQuery) Insert(ctx context.Context, user *db.User) (*db.User, error) {
	if m.insertErr != nil {
		return nil, m.insertErr
	}
	user.ID = 100
	m.inserted = user
	return user, nil
}
func (m *mockUserQuery) InsertWithTx(ctx context.Context, tx pgx.Tx, user *db.User) (*db.User, error) {
	return user, nil
}
func (m *mockUserQuery) Update(ctx context.Context, user *db.User, id int64) (*db.User, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return user, nil
}
func (m *mockUserQuery) UpdateAuthTime(ctx context.Context, id int64) (*db.User, error) {
	return nil, nil
}
func (m *mockUserQuery) UpdateLoginOrLogout(ctx context.Context, user *db.User, id int64) (*db.User, error) {
	return user, nil
}
func (m *mockUserQuery) Delete(ctx context.Context, id int64) error { return nil }
func (m *mockUserQuery) GetByEmailVerificationToken(ctx context.Context, token string) (*db.User, error) {
	return nil, nil
}
func (m *mockUserQuery) UpdateEmailVerification(ctx context.Context, userID int64, verified bool, token *string) error {
	return nil
}
func (m *mockUserQuery) UpdateVerificationCode(ctx context.Context, userID int64, code string, expiresAt time.Time) error {
	return nil
}
func (m *mockUserQuery) GetByEmailAndCode(ctx context.Context, email, code string) (*db.User, error) {
	return m.codeUser, m.codeErr
}
func (m *mockUserQuery) VerifyEmailByCode(ctx context.Context, userID int64) error { return m.verifyErr }
func (m *mockUserQuery) BeginTx(ctx context.Context) (pgx.Tx, error)               { return nil, m.beginErr }
func (m *mockUserQuery) SearchUsers(ctx context.Context, query string, limit, offset int) ([]*db.UserListItem, int, error) {
	return nil, 0, nil
}
func (m *mockUserQuery) FindUserIDByPhone(ctx context.Context, phone string) (*int64, error) {
	return nil, nil
}

func newUserUC(m *mockUserQuery) *UserUsecase {
	return NewUserUsecase(m, zap.NewNop(), "test-secret")
}

// ---- tests ----------------------------------------------------------------

func TestUser_Register(t *testing.T) {
	m := newMockUserQuery()
	uc := newUserUC(m)

	user, err := uc.Register(context.Background(), RegisterRequest{Email: "  NEW@Mail.RU ", Name: "Иван", Phone: "+79990000000"})
	if err != nil {
		t.Fatalf("register error: %v", err)
	}
	if user.Email != "new@mail.ru" {
		t.Errorf("email not normalized: %q", user.Email)
	}

	// дубликат
	m.exists = true
	if _, err := uc.Register(context.Background(), RegisterRequest{Email: "a@b.ru", Name: "X", Phone: "+7"}); err == nil {
		t.Error("expected duplicate error")
	}
	// honeypot
	if _, err := uc.Register(context.Background(), RegisterRequest{Email: "a@b.ru", Name: "X", Phone: "+7", Website: "bot"}); err == nil {
		t.Error("expected honeypot rejection")
	}
	// нет полей
	if _, err := uc.Register(context.Background(), RegisterRequest{Email: "a@b.ru"}); err == nil {
		t.Error("expected validation error")
	}
}

func TestUser_RegisterWithTransaction_BeginTxError(t *testing.T) {
	m := newMockUserQuery()
	uc := newUserUC(m)
	if _, err := uc.RegisterWithTransaction(context.Background(), RegisterRequest{Email: "x@y.ru", Name: "Имя", Phone: "+79990000000"}); err == nil {
		t.Error("expected error from BeginTx")
	}
}

func TestUser_Login(t *testing.T) {
	m := newMockUserQuery()
	m.byEmail["user@mail.ru"] = &db.User{ID: 1, Email: "user@mail.ru", Username: "user"}
	uc := newUserUC(m)

	user, err := uc.Login(context.Background(), LoginRequest{Email: "USER@mail.ru"})
	if err != nil {
		t.Fatalf("login error: %v", err)
	}
	if user.ID != 1 {
		t.Errorf("wrong user: %+v", user)
	}
	if _, err := uc.Login(context.Background(), LoginRequest{Email: "missing@mail.ru"}); err == nil {
		t.Error("expected not-found error")
	}
	if _, err := uc.Login(context.Background(), LoginRequest{Email: ""}); err == nil {
		t.Error("expected empty-email error")
	}
}

func TestUser_GetUserByEmail(t *testing.T) {
	m := newMockUserQuery()
	m.byEmail["a@b.ru"] = &db.User{ID: 5, Email: "a@b.ru"}
	uc := newUserUC(m)

	if _, err := uc.GetUserByEmail(context.Background(), "A@B.ru"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if _, err := uc.GetUserByEmail(context.Background(), "no@b.ru"); err == nil {
		t.Error("expected not found")
	}
}

func TestUser_VerifyEmailByCode(t *testing.T) {
	m := newMockUserQuery()
	m.codeUser = &db.User{ID: 7, Email: "v@b.ru"}
	uc := newUserUC(m)

	user, err := uc.VerifyEmailByCode(context.Background(), "v@b.ru", "123456")
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !user.EmailVerified {
		t.Error("EmailVerified should be true")
	}

	m.codeErr = errors.New("bad code")
	if _, err := uc.VerifyEmailByCode(context.Background(), "v@b.ru", "000000"); err == nil {
		t.Error("expected invalid-code error")
	}
}

func TestUser_JWTAndHelpers(t *testing.T) {
	uc := newUserUC(newMockUserQuery())
	tok, err := uc.GenerateJWTToken(1, "user", "u@b.ru")
	if err != nil || tok == "" {
		t.Fatalf("token gen failed: %v", err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Errorf("not a JWT: %q", tok)
	}
	if generateJTI() == generateJTI() {
		t.Error("JTI must be unique")
	}
	if generateSecret() == "" {
		t.Error("secret must be non-empty")
	}
	if t2, _ := generateJWTToken(1, "u", "e", "s"); t2 == "" {
		t.Error("generateJWTToken empty")
	}
}

func TestUser_Profile(t *testing.T) {
	m := newMockUserQuery()
	m.byID[1] = &db.User{ID: 1, Username: "user", Password: "secret-hash"}
	uc := newUserUC(m)

	p, err := uc.GetProfile(context.Background(), 1)
	if err != nil {
		t.Fatalf("profile error: %v", err)
	}
	if p.Password != "" {
		t.Error("password must be cleared in profile output")
	}
	if _, err := uc.GetProfile(context.Background(), 999); err == nil {
		t.Error("expected not found")
	}
}

func TestUser_UpdateProfile(t *testing.T) {
	m := newMockUserQuery()
	m.byID[1] = &db.User{ID: 1}
	uc := newUserUC(m)

	if _, err := uc.UpdateProfile(context.Background(), 1, map[string]interface{}{"full_name": "Пётр", "phone": "+7 999 123-45-67"}); err != nil {
		t.Errorf("valid update failed: %v", err)
	}
	if _, err := uc.UpdateProfile(context.Background(), 1, map[string]interface{}{"phone": "123"}); err == nil {
		t.Error("expected invalid phone error")
	}
	if _, err := uc.UpdateProfile(context.Background(), 1, map[string]interface{}{"full_name": strings.Repeat("я", 33)}); err == nil {
		t.Error("expected long-name error")
	}
}

func TestUser_ResetPassword(t *testing.T) {
	m := newMockUserQuery()
	m.byID[1] = &db.User{ID: 1}
	uc := newUserUC(m)

	if err := uc.ResetPassword(context.Background(), 1, "weak"); err == nil {
		t.Error("weak password accepted")
	}
	if err := uc.ResetPassword(context.Background(), 1, "Strong1Pass!"); err != nil {
		t.Errorf("strong password rejected: %v", err)
	}
}

func TestUser_IsPasswordStrong(t *testing.T) {
	uc := newUserUC(newMockUserQuery())
	strong := []string{"Abcdef1!", "GoodPass9", "Zzzz9!aa"}
	weak := []string{"short", "alllowercase", "12345678", "        "}
	for _, p := range strong {
		if !uc.isPasswordStrong(p) {
			t.Errorf("%q should be strong", p)
		}
	}
	for _, p := range weak {
		if uc.isPasswordStrong(p) {
			t.Errorf("%q should be weak", p)
		}
	}
}

func TestUser_SaveAndClearCode(t *testing.T) {
	m := newMockUserQuery()
	uc := newUserUC(m)
	if err := uc.SaveVerificationCode(context.Background(), 1, "123456"); err != nil {
		t.Errorf("save code error: %v", err)
	}
	if err := uc.ClearVerificationCode(context.Background(), 1); err != nil {
		t.Errorf("clear code error: %v", err)
	}
	if _, err := uc.GetByEmailAndCode(context.Background(), "a@b.ru", "1"); err != nil {
		t.Errorf("GetByEmailAndCode error: %v", err)
	}
}
