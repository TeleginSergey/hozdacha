package services

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strconv"

	"github.com/TeleginSergey/hozdacha/internal/config"
	"go.uber.org/zap"
)

type EmailService struct {
	cfg    config.SMTPConfig
	logger *zap.Logger
}

func NewEmailService(cfg config.SMTPConfig, logger *zap.Logger) *EmailService {
	return &EmailService{
		cfg:    cfg,
		logger: logger,
	}
}

// SendVerificationCode отправляет код верификации на email
func (s *EmailService) SendVerificationCode(to, name, code string) error {
	subject := "Код подтверждения регистрации - ХозДача"
	body := fmt.Sprintf(`Здравствуйте, %s!

Код подтверждения: %s

Введите этот код в поле подтверждения на сайте для завершения регистрации.
Код действителен в течение 30 минут.

Если вы не регистрировались на сайте hozdacha.ru, проигнорируйте это письмо.

С уважением,
Команда Хозяйкин Дом
`, name, code)

	return s.sendEmail(to, subject, body)
}

// SendPasswordResetCode отправляет код для сброса пароля
func (s *EmailService) SendPasswordResetCode(to, name, code string) error {
	subject := "Сброс пароля - hozdacha.ru"
	body := fmt.Sprintf(`Здравствуйте, %s!

Код для сброса пароля: %s

Введите этот код на странице сброса пароля.
Код действителен в течение 30 минут.

Если вы не запрашивали сброс пароля, проигнорируйте это письмо.

С уважением,
Команда hozdacha.ru
`, name, code)

	return s.sendEmail(to, subject, body)
}

// SendOrderConfirmation отправляет подтверждение заказа
func (s *EmailService) SendOrderConfirmation(to, name string, orderID int64, total float64) error {
	subject := "Подтверждение заказа - Хозяйкин Дом"
	body := fmt.Sprintf(`Здравствуйте, %s!

Ваш заказ №%d успешно оформлен.

Сумма заказа: %.2f руб.

В ближайшее время с вами свяжется менеджер для подтверждения заказа.

С уважением,
Команда Хозяйкин Дом
`, name, orderID, total)

	return s.sendEmail(to, subject, body)
}

// sendEmail отправляет email через SMTPS
func (s *EmailService) sendEmail(to, subject, body string) error {
	if s.cfg.Host == "" {
		s.logger.Warn("SMTP host not configured, email not sent",
			zap.String("to", to),
			zap.String("subject", subject))
		return nil // Не возвращаем ошибку, чтобы не блокировать регистрацию
	}

	from := s.cfg.From
	if from == "" {
		from = s.cfg.Username
	}

	s.logger.Info("Sending email",
		zap.String("to", to),
		zap.String("from", from),
		zap.String("subject", subject),
		zap.String("smtp_host", s.cfg.Host),
		zap.Int("smtp_port", s.cfg.Port),
		zap.Bool("use_tls", s.cfg.UseTLS))

	// Формируем сообщение
	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"From: %s\r\n"+
		"Subject: %s\r\n"+
		"Content-Type: text/plain; charset=UTF-8\r\n"+
		"\r\n"+
		"%s\r\n", to, from, subject, body))

	// Настройка TLS
	tlsConfig := &tls.Config{
		ServerName: s.cfg.Host,
	}

	smtpHost := s.cfg.Host

	// Подключаемся к SMTP серверу
	addr := smtpHost + ":" + strconv.Itoa(s.cfg.Port)

	var err error
	if s.cfg.UseTLS && s.cfg.Port == 465 {
		// SMTPS (SSL/TLS сразу) — Yandex/Gmail port 465
		err = s.sendSMTPS(addr, tlsConfig, from, to, msg)
	} else if s.cfg.UseTLS && s.cfg.Port == 587 {
		// SMTP с STARTTLS (порт 587) — Yandex/Gmail/Mail.ru port 587
		// sendPlain не работает: smtp.PlainAuth отказывает без TLS
		err = s.sendSTARTTLS(addr, tlsConfig, from, to, msg)
	} else {
		// Plain SMTP (порт 25, локальный Postfix)
		err = s.sendPlain(addr, from, to, msg)
	}

	if err != nil {
		s.logger.Error("Failed to send email",
			zap.String("to", to),
			zap.Error(err))
		return fmt.Errorf("failed to send email: %w", err)
	}

	s.logger.Info("Email sent successfully", zap.String("to", to))
	return nil
}

// sendSMTPS отправляет email через SMTPS (SSL/TLS)
func (s *EmailService) sendSMTPS(addr string, tlsConfig *tls.Config, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect via TLS: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// Аутентификация
	if s.cfg.Username != "" && s.cfg.Password != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}
	}

	// Отправка письма
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("failed to set recipient: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to open data connection: %w", err)
	}

	_, err = w.Write(msg)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("failed to close data connection: %w", err)
	}

	return client.Quit()
}

// sendSTARTTLS отправляет email через SMTP с STARTTLS
func (s *EmailService) sendSTARTTLS(addr string, tlsConfig *tls.Config, from, to string, msg []byte) error {
	// Подключаемся к серверу
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Close()

	// Приветствие (EHLO от имени клиента, не сервера)
	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("failed to send hello: %w", err)
	}

	// Проверяем поддержку STARTTLS
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("failed to start TLS: %w", err)
		}
	}

	// Аутентификация
	if s.cfg.Username != "" && s.cfg.Password != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}
	}

	// Отправка письма
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("failed to set recipient: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to open data connection: %w", err)
	}

	_, err = w.Write(msg)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("failed to close data connection: %w", err)
	}

	return client.Quit()
}
