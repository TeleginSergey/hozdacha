package db

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/elgris/stom"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const UsersTable = "users"

const (
	UsersID                     = "users_id_pk"
	UsersUsername               = "users_username"
	UsersPasswordHash           = "users_password_hash"
	UsersEmail                  = "users_email"
	UsersRoleID                 = "users_role_id_fk"
	UsersAccessTokenSecret      = "users_access_token_secret"
	UsersRefreshTokenSecret     = "users_refresh_token_secret"
	UsersAuthTime               = "users_auth_time"
	UsersCreatedAt              = "users_created_at"
	UsersUpdatedAt              = "users_updated_at"
	UsersEmailVerified          = "users_email_verified"
	UsersEmailVerificationToken = "users_email_verification_token"
	UsersEmailVerificationCode  = "users_email_verification_code"
	UsersVerificationExpiresAt  = "users_verification_expires_at"
	UsersWebsite                = "users_website"
)

type User struct {
	ID                     int64      `db:"users_id_pk" json:"id"`
	Username               string     `db:"users_username" insert:"users_username" update:"users_username" json:"username"`
	Password               string     `db:"users_password_hash" insert:"users_password_hash" update:"users_password_hash" json:"-"`
	Email                  string     `db:"users_email" insert:"users_email" json:"email"`
	RoleID                 int64      `db:"users_roles_id_fk" insert:"users_roles_id_fk" json:"role_id"`
	AccessTokenSecret      *string    `db:"users_access_token_secret" insert:"users_access_token_secret" json:"-"`
	RefreshTokenSecret     *string    `db:"users_refresh_token_secret" insert:"users_refresh_token_secret" json:"-"`
	AccessTokenJTI         *string    `db:"users_access_token_jti" updateAuth:"users_access_token_jti" json:"-"`
	RefreshTokenJTI        *string    `db:"users_refresh_token_jti" updateAuth:"users_refresh_token_jti" json:"-"`
	AuthTime               *time.Time `db:"users_auth_time" insert:"users_auth_time" json:"-"`
	CreatedAt              *time.Time `db:"users_created_at" json:"created_at"`
	UpdatedAt              *time.Time `db:"users_updated_at" updateAuth:"users_updated_at" update:"users_updated_at" json:"updated_at"`
	EmailVerified          bool       `db:"users_email_verified" insert:"users_email_verified" json:"email_verified"`
	EmailVerificationToken *string    `db:"users_email_verification_token" insert:"users_email_verification_token" json:"-"`
	EmailVerificationCode  *string    `db:"users_email_verification_code" insert:"users_email_verification_code" json:"-"`
	VerificationExpiresAt  *time.Time `db:"users_verification_expires_at" insert:"users_verification_expires_at" json:"-"`
	Website                *string    `db:"users_website" insert:"users_website" json:"-"` // Honeypot field
	FullName               *string    `db:"users_full_name" insert:"users_full_name" update:"users_full_name" json:"full_name"`
	Phone                  *string    `db:"users_phone" insert:"users_phone" update:"users_phone" json:"phone"`
}

var (
	stomUserSelect     = stom.MustNewStom(User{}).SetTag(selectTag)
	stomUserInsert     = stom.MustNewStom(User{}).SetTag(insertTag)
	stomUserUpdate     = stom.MustNewStom(User{}).SetTag(updateTag)
	stomUserAuthUpdate = stom.MustNewStom(User{}).SetTag(updateAuthTag)
)

func (u *User) columns(pref string) []string {
	return colNamesWithPref(stomUserSelect.TagValues(), pref)
}

type UserQuery interface {
	GetByID(ctx context.Context, id int64) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	ExistsByUsernameOrEmail(ctx context.Context, username string, email string) (bool, error)
	Insert(ctx context.Context, user *User) (*User, error)
	InsertWithTx(ctx context.Context, tx pgx.Tx, user *User) (*User, error)
	Update(ctx context.Context, user *User, id int64) (*User, error)
	UpdateAuthTime(ctx context.Context, id int64) (*User, error)
	UpdateLoginOrLogout(ctx context.Context, user *User, id int64) (*User, error)
	Delete(ctx context.Context, id int64) error
	GetByEmailVerificationToken(ctx context.Context, token string) (*User, error)
	UpdateEmailVerification(ctx context.Context, userID int64, verified bool, token *string) error
	UpdateVerificationCode(ctx context.Context, userID int64, code string, expiresAt time.Time) error
	GetByEmailAndCode(ctx context.Context, email string, code string) (*User, error)
	VerifyEmailByCode(ctx context.Context, userID int64) error
	BeginTx(ctx context.Context) (pgx.Tx, error)
	// SearchUsers — поиск пользователей по username/email/phone (из заказов) с пагинацией.
	SearchUsers(ctx context.Context, query string, limit, offset int) ([]*UserListItem, int, error)
	// FindUserIDByPhone — находит ID пользователя по телефону из его заказов.
	FindUserIDByPhone(ctx context.Context, phone string) (*int64, error)
}

type userQuery struct {
	runner *pgxpool.Pool
	sq     squirrel.StatementBuilderType
	logger *zap.Logger
}

func (u *userQuery) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return u.runner.Begin(ctx)
}

func (u *userQuery) GetByEmailVerificationToken(ctx context.Context, token string) (*User, error) {
	u.logger.Debug("Getting user by email verification token", zap.String("token", token))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := u.sq.Select(UsersID, UsersUsername, UsersEmail, UsersEmailVerified, UsersEmailVerificationToken).
		From(UsersTable).
		Where(squirrel.Eq{UsersEmailVerificationToken: token}).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	var user User
	err = pgxscan.Get(ctx, u.runner, &user, qb, args...)
	if err != nil {
		u.logger.Error("Failed to get user by email verification token", zap.Error(err))
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	return &user, nil
}

func (u *userQuery) UpdateEmailVerification(ctx context.Context, userID int64, verified bool, token *string) error {
	u.logger.Debug("Updating email verification", zap.Int64("user_id", userID), zap.Bool("verified", verified))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb := u.sq.Update(UsersTable).
		Where(squirrel.Eq{UsersID: userID})

	if verified {
		qb = qb.Set(UsersEmailVerified, true)
		if token != nil {
			qb = qb.Set(UsersEmailVerificationToken, nil)
		}
	} else {
		if token != nil {
			qb = qb.Set(UsersEmailVerificationToken, token)
		}
	}

	sql, args, err := qb.ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return fmt.Errorf("failed to build query: %w", err)
	}

	_, err = u.runner.Exec(ctx, sql, args...)
	if err != nil {
		u.logger.Error("Failed to update email verification", zap.Int64("user_id", userID), zap.Error(err))
		return fmt.Errorf("failed to execute query: %w", err)
	}

	u.logger.Info("Email verification updated successfully", zap.Int64("user_id", userID))
	return nil
}

func NewUserQuery(runner *pgxpool.Pool, sq squirrel.StatementBuilderType, logger *zap.Logger) UserQuery {
	return &userQuery{
		runner: runner,
		sq:     sq,
		logger: logger,
	}
}

func (u *userQuery) GetByID(ctx context.Context, id int64) (*User, error) {
	u.logger.Debug("Fetching user by ID", zap.Int64("user_id", id))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	user := &User{}
	qb, args, err := u.sq.Select(user.columns("")...).
		From(UsersTable).
		Where(squirrel.Eq{UsersID: id}).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, u.runner, user, qb, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			u.logger.Warn("Database error",
				zap.Int64("user_id", id),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			u.logger.Warn("Failed to fetch user", zap.Int64("user_id", id), zap.Error(err))
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	u.logger.Info("User fetched successfully", zap.Int64("user_id", id))
	return user, nil
}

func (u *userQuery) GetByUsername(ctx context.Context, username string) (*User, error) {
	u.logger.Debug("Fetching user by username", zap.String("username", username))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	user := &User{}
	qb, args, err := u.sq.Select(user.columns("")...).
		From(UsersTable).
		Where(squirrel.Eq{UsersUsername: username}).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, u.runner, user, qb, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			u.logger.Warn("Database error",
				zap.String("username", username),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			u.logger.Warn("Failed to fetch user", zap.String("username", username), zap.Error(err))
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	u.logger.Info("User fetched successfully", zap.String("username", username))
	return user, nil
}

func (u *userQuery) GetByEmail(ctx context.Context, email string) (*User, error) {
	u.logger.Debug("Fetching user by email", zap.String("email", email))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	user := &User{}
	qb, args, err := u.sq.Select(user.columns("")...).
		From(UsersTable).
		// Email регистронезависим: сравниваем по нижнему регистру, чтобы найти
		// и записи со смешанным регистром, заведённые до нормализации.
		Where(squirrel.Expr("LOWER("+UsersEmail+") = LOWER(?)", email)).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, u.runner, user, qb, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			u.logger.Warn("Database error",
				zap.String("email", email),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			u.logger.Warn("Failed to fetch user", zap.String("email", email), zap.Error(err))
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	u.logger.Info("User fetched successfully", zap.String("email", email))
	return user, nil
}

func (u *userQuery) ExistsByUsernameOrEmail(ctx context.Context, username, email string) (bool, error) {
	u.logger.Debug("Checking if user exists by username or email",
		zap.String("username", username),
		zap.String("email", email))

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var count int
	query, args, err := u.sq.Select("COUNT(*)").
		From(UsersTable).
		Where(squirrel.Or{
			squirrel.Eq{UsersUsername: username},
			// Email — регистронезависимо.
			squirrel.Expr("LOWER("+UsersEmail+") = LOWER(?)", email),
		}).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return false, fmt.Errorf("failed to build query: %w", err)
	}

	err = u.runner.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			u.logger.Warn("Database error",
				zap.String("username", username),
				zap.String("email", email),
				zap.String("error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			u.logger.Error("Failed to check user existence",
				zap.String("username", username),
				zap.String("email", email),
				zap.Error(err),
			)
		}
		return false, fmt.Errorf("failed to execute query: %w", err)
	}

	exists := count > 0
	if exists {
		u.logger.Info("User already exists",
			zap.String("username", username),
			zap.String("email", email),
		)
	}
	return exists, nil
}

func (u *userQuery) Insert(ctx context.Context, user *User) (*User, error) {
	u.logger.Debug("Inserting user", zap.Any("user", user))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	insertMap, err := stomUserInsert.ToMap(user)
	if err != nil {
		u.logger.Error("Failed to map struct", zap.Error(err))
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := u.sq.Insert(UsersTable).
		SetMap(insertMap).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, u.runner, user, qb, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			u.logger.Warn("Database error",
				zap.Any("user", user),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			u.logger.Error("Failed to insert user", zap.Any("user", user), zap.Error(err))
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	u.logger.Info("User inserted successfully", zap.Int64("user_id", user.ID))
	return user, nil
}

func (u *userQuery) InsertWithTx(ctx context.Context, tx pgx.Tx, user *User) (*User, error) {
	u.logger.Debug("Inserting user with transaction", zap.Any("user", user))

	insertMap, err := stomUserInsert.ToMap(user)
	if err != nil {
		u.logger.Error("Failed to map struct", zap.Error(err))
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := u.sq.Insert(UsersTable).
		SetMap(insertMap).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	// Создаем адаптер для транзакции, который реализует интерфейс pgxscan.Querier
	txAdapter := &txQuerier{tx: tx}
	err = pgxscan.Get(ctx, txAdapter, user, qb, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			u.logger.Warn("Database error",
				zap.Any("user", user),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			u.logger.Error("Failed to insert user with transaction", zap.Any("user", user), zap.Error(err))
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	u.logger.Info("User inserted successfully with transaction", zap.Int64("user_id", user.ID))
	return user, nil
}

// txQuerier адаптер для транзакции, реализующий интерфейс pgxscan.Querier
type txQuerier struct {
	tx pgx.Tx
}

func (t *txQuerier) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

func (t *txQuerier) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, sql, args...)
}

func (t *txQuerier) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return t.tx.QueryRow(ctx, sql, args...)
}

func (u *userQuery) Update(ctx context.Context, user *User, id int64) (*User, error) {
	u.logger.Debug("Updating user", zap.Int64("user_id", id))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updateMap, err := stomUserUpdate.ToMap(user)
	if err != nil {
		u.logger.Error("Failed to map struct", zap.Error(err))
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := u.sq.Update(UsersTable).
		SetMap(updateMap).
		Where(squirrel.Eq{UsersID: id}).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, u.runner, user, qb, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			u.logger.Warn("Database error",
				zap.Int64("user_id", user.ID),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			u.logger.Error("Failed to update user", zap.Int64("user_id", user.ID), zap.Error(err))
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	u.logger.Info("User updated successfully", zap.Int64("user_id", user.ID))
	return user, nil
}

func (u *userQuery) Delete(ctx context.Context, id int64) error {
	u.logger.Debug("Deleting user", zap.Int64("user_id", id))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := u.sq.Delete(UsersTable).
		Where(squirrel.Eq{UsersID: id}).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return fmt.Errorf("failed to build query: %w", err)
	}

	result, err := u.runner.Exec(ctx, qb, args...)
	if err != nil {
		u.logger.Error("Failed to delete user", zap.Int64("user_id", id), zap.Error(err))
		return fmt.Errorf("failed to execute query: %w", err)
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		u.logger.Warn("No user found to delete", zap.Int64("user_id", id))
		return fmt.Errorf("no user found with id %d", id)
	}

	u.logger.Info("User deleted successfully", zap.Int64("user_id", id))
	return nil
}

func (u *userQuery) UpdateLoginOrLogout(ctx context.Context, user *User, id int64) (*User, error) {
	u.logger.Debug("Updating user for auth", zap.Int64("user_id", id))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updateMap, err := stomUserAuthUpdate.ToMap(user)
	if err != nil {
		u.logger.Error("Failed to map struct", zap.Error(err))
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := u.sq.Update(UsersTable).
		SetMap(updateMap).
		Where(squirrel.Eq{UsersID: id}).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, u.runner, user, qb, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			u.logger.Warn("Database error",
				zap.Int64("user_id", user.ID),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			u.logger.Error("Failed to update user", zap.Int64("user_id", user.ID), zap.Error(err))
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	u.logger.Info("User updated successfully", zap.Int64("user_id", user.ID))
	return user, nil
}

func (u *userQuery) UpdateAuthTime(ctx context.Context, id int64) (*User, error) {
	u.logger.Debug("Updating user auth time", zap.Int64("user_id", id))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user User
	qb, args, err := u.sq.Update(UsersTable).
		Set(UsersAuthTime, time.Now()).
		Where(squirrel.Eq{UsersID: id}).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, u.runner, &user, qb, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			u.logger.Warn("Database error",
				zap.Int64("user_id", id),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			u.logger.Error("Failed to update user auth time",
				zap.Int64("user_id", id),
				zap.Error(err),
			)
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	u.logger.Info("User auth time updated successfully",
		zap.Int64("user_id", user.ID),
		zap.Time("new_auth_time", *user.AuthTime),
	)
	return &user, nil
}

func (u *userQuery) DeleteUnverifiedUsers(ctx context.Context, cutoff time.Time) (int64, error) {
	u.logger.Debug("Deleting unverified users created before", zap.Time("cutoff", cutoff))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	qb, args, err := u.sq.Delete(UsersTable).
		Where(squirrel.And{
			squirrel.Eq{UsersEmailVerified: false},
			squirrel.Lt{UsersCreatedAt: cutoff},
		}).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build delete query", zap.Error(err))
		return 0, fmt.Errorf("failed to build query: %w", err)
	}

	result, err := u.runner.Exec(ctx, qb, args...)
	if err != nil {
		u.logger.Error("Failed to delete unverified users", zap.Error(err))
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	rowsAffected := result.RowsAffected()
	u.logger.Info("Deleted unverified users", zap.Int64("count", rowsAffected))
	return rowsAffected, nil
}

func (u *userQuery) UpdateVerificationCode(ctx context.Context, userID int64, code string, expiresAt time.Time) error {
	u.logger.Debug("Updating verification code", zap.Int64("user_id", userID))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := u.sq.Update(UsersTable).
		Set(UsersEmailVerificationCode, code).
		Set(UsersVerificationExpiresAt, expiresAt).
		Where(squirrel.Eq{UsersID: userID}).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return fmt.Errorf("failed to build query: %w", err)
	}

	_, err = u.runner.Exec(ctx, qb, args...)
	if err != nil {
		u.logger.Error("Failed to update verification code", zap.Int64("user_id", userID), zap.Error(err))
		return fmt.Errorf("failed to execute query: %w", err)
	}

	u.logger.Info("Verification code updated", zap.Int64("user_id", userID))
	return nil
}

func (u *userQuery) GetByEmailAndCode(ctx context.Context, email string, code string) (*User, error) {
	u.logger.Debug("Getting user by email and code", zap.String("email", email))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := u.sq.Select(stomUserSelect.TagValues()...).
		From(UsersTable).
		Where(squirrel.And{
			squirrel.Expr("LOWER("+UsersEmail+") = LOWER(?)", email),
			squirrel.Eq{UsersEmailVerificationCode: code},
			squirrel.Gt{UsersVerificationExpiresAt: time.Now()},
		}).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	var user User
	err = pgxscan.Get(ctx, u.runner, &user, qb, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("invalid or expired verification code")
		}
		u.logger.Error("Failed to get user by email and code", zap.Error(err))
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	return &user, nil
}

func (u *userQuery) VerifyEmailByCode(ctx context.Context, userID int64) error {
	u.logger.Debug("Verifying email by code", zap.Int64("user_id", userID))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := u.sq.Update(UsersTable).
		Set(UsersEmailVerified, true).
		Set(UsersEmailVerificationCode, nil).
		Set(UsersVerificationExpiresAt, nil).
		Where(squirrel.Eq{UsersID: userID}).
		ToSql()
	if err != nil {
		u.logger.Error("Failed to build query", zap.Error(err))
		return fmt.Errorf("failed to build query: %w", err)
	}

	_, err = u.runner.Exec(ctx, qb, args...)
	if err != nil {
		u.logger.Error("Failed to verify email", zap.Int64("user_id", userID), zap.Error(err))
		return fmt.Errorf("failed to execute query: %w", err)
	}

	u.logger.Info("Email verified successfully", zap.Int64("user_id", userID))
	return nil
}

func GenerateSecretKey() (string, error) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", key), nil
}
