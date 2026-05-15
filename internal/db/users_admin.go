package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
)

// UserListItem — данные клиента + краткая агрегация по заказам (для списка клиентов в админке).
type UserListItem struct {
	ID            int64      `db:"users_id_pk" json:"id"`
	Username      string     `db:"users_username" json:"username"`
	Email         string     `db:"users_email" json:"email"`
	FullName      *string    `db:"users_full_name" json:"full_name,omitempty"`
	Phone         *string    `db:"users_phone" json:"phone,omitempty"`
	EmailVerified bool       `db:"users_email_verified" json:"email_verified"`
	CreatedAt     *time.Time `db:"users_created_at" json:"created_at"`
	OrdersCount   int        `db:"orders_count" json:"orders_count"`
	LastOrderAt   *time.Time `db:"last_order_at" json:"last_order_at,omitempty"`
}

// SearchUsers ищет клиентов по username/email/телефону из заказов.
// Если query пустой — возвращает всех пользователей с пагинацией.
// limit ограничен 200.
func (u *userQuery) SearchUsers(ctx context.Context, query string, limit, offset int) ([]*UserListItem, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	q := strings.TrimSpace(query)
	whereSQL := ""
	args := []any{}
	if q != "" {
		// Ищем по username, email, full_name, phone в самой users + по phone из заказов
		// (если телефон не был сохранён в профиле, но был указан при оформлении).
		whereSQL = `
			WHERE (
				u.users_username ILIKE $1
				OR u.users_email ILIKE $1
				OR COALESCE(u.users_full_name, '') ILIKE $1
				OR COALESCE(u.users_phone, '') ILIKE $1
				OR EXISTS (
					SELECT 1 FROM orders o
					WHERE o.orders_user_id_fk = u.users_id_pk
					  AND o.orders_phone ILIKE $1
				)
			)`
		args = append(args, "%"+q+"%")
	}

	// Список с агрегациями по заказам.
	listArgsBase := len(args)
	listSQL := `
		SELECT
			u.users_id_pk, u.users_username, u.users_email, u.users_full_name, u.users_phone,
			u.users_email_verified, u.users_created_at,
			COUNT(o.orders_id_pk) AS orders_count,
			MAX(o.orders_created_at) AS last_order_at
		FROM users u
		LEFT JOIN orders o ON o.orders_user_id_fk = u.users_id_pk
		` + whereSQL + `
		GROUP BY u.users_id_pk
		ORDER BY u.users_created_at DESC NULLS LAST
		LIMIT $` + itoa(listArgsBase+1) + ` OFFSET $` + itoa(listArgsBase+2)
	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, limit, offset)

	var users []*UserListItem
	if err := pgxscan.Select(ctx, u.runner, &users, listSQL, listArgs...); err != nil {
		return nil, 0, fmt.Errorf("failed to search users: %w", err)
	}

	// Общий count для пагинации.
	countSQL := `SELECT COUNT(*) FROM ` + UsersTable + ` u ` + whereSQL
	var total int
	if err := u.runner.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}
	return users, total, nil
}

// FindUserIDByPhone находит ID пользователя по последнему заказу с этим телефоном.
// Используется для поиска "по телефону клиента" из админки/кассы.
// Возвращает (nil, nil) если не найдено.
func (u *userQuery) FindUserIDByPhone(ctx context.Context, phone string) (*int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return nil, nil
	}
	sqlText, args, err := u.sq.Select(OrdersUserID).
		From(OrdersTable).
		Where(squirrel.Eq{OrdersPhone: phone}).
		Where(squirrel.NotEq{OrdersUserID: nil}).
		OrderBy(OrdersCreatedAt + " DESC").
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build find by phone: %w", err)
	}
	var userID *int64
	if err := u.runner.QueryRow(ctx, sqlText, args...).Scan(&userID); err != nil {
		// pgx ErrNoRows возвращается через scany — но без import-а специфики используем строку.
		if strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, fmt.Errorf("find by phone: %w", err)
	}
	return userID, nil
}

func itoa(n int) string {
	// Простая инлайн-конвертация без пакета strconv, чтобы не тянуть импорт ради 1 строки.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
