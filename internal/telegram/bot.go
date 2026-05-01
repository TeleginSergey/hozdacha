package telegram

import (
	"context"
	"fmt"
	"strconv"

	"go.uber.org/zap"
	"gopkg.in/telebot.v3"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

type Bot struct {
	bot    *telebot.Bot
	chatID int64
	logger *zap.Logger
}

func NewBot(token string, chatIDStr string, logger *zap.Logger) (*Bot, error) {
	if token == "" {
		logger.Warn("Telegram bot token not provided, bot will not be initialized")
		return nil, nil
	}

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		logger.Warn("Invalid Telegram chat ID, bot will not be initialized", zap.Error(err))
		return nil, nil
	}

	bot, err := telebot.NewBot(telebot.Settings{
		Token: token,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	logger.Info("Telegram bot initialized successfully")

	return &Bot{
		bot:    bot,
		chatID: chatID,
		logger: logger,
	}, nil
}

func (b *Bot) SendOrderNotification(ctx context.Context, order *db.Order) error {
	if b == nil {
		return nil
	}

	message := fmt.Sprintf(
		"🛒 *Новый заказ #%d*\n\n"+
			"👤 Клиент: %s\n"+
			"📞 Телефон: %s\n"+
			"💰 Сумма: %.2f руб.\n"+
			"📦 Товаров: %d\n",
		order.ID,
		order.CustomerName,
		order.Phone,
		order.TotalPrice,
		len(order.Items),
	)

	if order.Address != nil && *order.Address != "" {
		message += fmt.Sprintf("📍 Адрес: %s\n", *order.Address)
	}

	if order.Comment != nil && *order.Comment != "" {
		message += fmt.Sprintf("💬 Комментарий: %s\n", *order.Comment)
	}

	message += "\n*Товары:*\n"
	for i, item := range order.Items {
		message += fmt.Sprintf("%d. Товар ID: %d, Кол-во: %d, Цена: %.2f руб.\n",
			i+1, item.ProductID, item.Quantity, item.Price*float64(item.Quantity))
	}

	_, err := b.bot.Send(telebot.ChatID(b.chatID), message, telebot.ModeMarkdown)
	if err != nil {
		b.logger.Error("Failed to send telegram message", zap.Error(err))
		return fmt.Errorf("failed to send telegram message: %w", err)
	}

	b.logger.Info("Order notification sent to Telegram", zap.Int64("order_id", order.ID))
	return nil
}

func (b *Bot) Start() {
	if b == nil {
		return
	}

	b.bot.Handle("/start", func(c telebot.Context) error {
		return c.Send("Привет! Я бот для уведомлений о новых заказах.")
	})

	go b.bot.Start()
	b.logger.Info("Telegram bot started")
}

func (b *Bot) Stop() {
	if b == nil {
		return
	}
	b.bot.Stop()
}
