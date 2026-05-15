package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const OrderEventsTable = "order_events"

const (
	OrderEventsID          = "order_events_id_pk"
	OrderEventsOrderID     = "order_events_order_id_fk"
	OrderEventsType        = "order_events_type"
	OrderEventsActorUserID = "order_events_actor_user_id_fk"
	OrderEventsPayload     = "order_events_payload"
	OrderEventsCreatedAt   = "order_events_created_at"
)

// Типы событий по жизненному циклу заказа.
const (
	OrderEventCreated        = "created"
	OrderEventShipped        = "shipped"
	OrderEventCancelled      = "cancelled"
	OrderEventExpired        = "expired"
	OrderEventMoyskladSynced = "moysklad_synced"
	OrderEventMoyskladFailed = "moysklad_failed"
)

// OrderEvent — запись audit log'а заказа.
type OrderEvent struct {
	ID          int64           `db:"order_events_id_pk" json:"id"`
	OrderID     int64           `db:"order_events_order_id_fk" json:"order_id"`
	Type        string          `db:"order_events_type" json:"type"`
	ActorUserID *int64          `db:"order_events_actor_user_id_fk" json:"actor_user_id,omitempty"`
	Payload     json.RawMessage `db:"order_events_payload" json:"payload,omitempty"`
	CreatedAt   time.Time       `db:"order_events_created_at" json:"created_at"`
}

type OrderEventQuery interface {
	// Insert добавляет событие audit log'а. payload может быть nil.
	Insert(ctx context.Context, orderID int64, eventType string, actorUserID *int64, payload any) error
	// ListByOrderID возвращает все события заказа в хронологическом порядке (новые сверху).
	ListByOrderID(ctx context.Context, orderID int64) ([]*OrderEvent, error)
}

type orderEventQuery struct {
	runner *pgxpool.Pool
	sq     squirrel.StatementBuilderType
	logger *zap.Logger
}

func NewOrderEventQuery(runner *pgxpool.Pool, logger *zap.Logger) OrderEventQuery {
	return &orderEventQuery{
		runner: runner,
		sq:     squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
		logger: logger,
	}
}

func (q *orderEventQuery) Insert(ctx context.Context, orderID int64, eventType string, actorUserID *int64, payload any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var payloadJSON []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal event payload: %w", err)
		}
		payloadJSON = b
	}

	qb, args, err := q.sq.Insert(OrderEventsTable).
		Columns(OrderEventsOrderID, OrderEventsType, OrderEventsActorUserID, OrderEventsPayload).
		Values(orderID, eventType, actorUserID, payloadJSON).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build insert event query: %w", err)
	}
	if _, err := q.runner.Exec(ctx, qb, args...); err != nil {
		return fmt.Errorf("failed to insert order event: %w", err)
	}
	return nil
}

func (q *orderEventQuery) ListByOrderID(ctx context.Context, orderID int64) ([]*OrderEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := q.sq.Select(
		OrderEventsID, OrderEventsOrderID, OrderEventsType,
		OrderEventsActorUserID, OrderEventsPayload, OrderEventsCreatedAt,
	).
		From(OrderEventsTable).
		Where(squirrel.Eq{OrderEventsOrderID: orderID}).
		OrderBy(OrderEventsCreatedAt + " DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list events query: %w", err)
	}

	var events []*OrderEvent
	if err := pgxscan.Select(ctx, q.runner, &events, qb, args...); err != nil {
		return nil, fmt.Errorf("failed to list order events: %w", err)
	}
	return events, nil
}
