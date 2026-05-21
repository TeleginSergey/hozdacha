package services

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net/smtp"
	"strconv"
	"strings"

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

// emailWrap оборачивает содержимое в красивый HTML-шаблон письма.
func emailWrap(title, bodyHTML string) string {
	return `<!DOCTYPE html><html lang="ru"><head><meta charset="UTF-8"></head>` +
		`<body style="margin:0;padding:0;background:#f0f2f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif">` +
		`<table width="100%" cellpadding="0" cellspacing="0" style="background:#f0f2f5;padding:30px 0"><tr><td align="center">` +
		`<table width="480" cellpadding="0" cellspacing="0" style="background:#fff;border-radius:12px;overflow:hidden;max-width:480px;width:100%">` +
		`<tr><td style="background:#4A7C59;padding:24px 30px;text-align:center">` +
		`<span style="color:#fff;font-size:20px;font-weight:700;letter-spacing:0.5px">` + title + `</span></td></tr>` +
		`<tr><td style="padding:30px">` + bodyHTML + `</td></tr>` +
		`<tr><td style="padding:0 30px 24px;color:#999;font-size:12px;text-align:center;line-height:1.5">` +
		`Если вы не совершали это действие, просто проигнорируйте письмо.<br>© Хозяйкин Дом, hozdacha.ru</td></tr>` +
		`</table></td></tr></table></body></html>`
}

// buildMsg собирает разряд письма с правильными MIME-заголовками и base64-телом.
func buildMsg(from, to, subject, body string, html bool) []byte {
	contentType := "text/plain; charset=UTF-8"
	if html {
		contentType = "text/html; charset=UTF-8"
	}
	encodedSubject := mime.QEncoding.Encode("UTF-8", subject)
	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))
	var b strings.Builder
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("Subject: " + encodedSubject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: " + contentType + "\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	b.WriteString(encodedBody)
	return []byte(b.String())
}

// codeDigits рендерит 6-значный код крупными цифрами в отдельных блоках.
func codeDigits(code string) string {
	var b strings.Builder
	for _, ch := range code {
		b.WriteString(`<td style="width:48px;height:60px;background:#f7f9fc;border-radius:8px;text-align:center;vertical-align:middle;font-size:28px;font-weight:800;color:#2d3a2c;font-family:'Courier New',monospace;letter-spacing:2px">`)
		b.WriteRune(ch)
		b.WriteString(`</td>`)
	}
	return `<table cellpadding="0" cellspacing="0" style="margin:20px auto"><tr>` + b.String() + `</tr></table>`
}

// SendVerificationCode отправляет код верификации на email
func (s *EmailService) SendVerificationCode(to, name, code string) error {
	greeting := name
	if greeting == "" {
		greeting = "здравствуйте"
	} else {
		greeting = "здравствуйте, " + name
	}
	body := `<p style="color:#444;font-size:15px;line-height:1.6;margin:0 0 8px">` + greeting + `!</p>` +
		`<p style="color:#555;font-size:14px;line-height:1.6;margin:0 0 20px">Для завершения регистрации введите этот код на сайте:</p>` +
		codeDigits(code) +
		`<p style="color:#888;font-size:13px;text-align:center;margin:0">Код действителен 30 минут</p>`
	return s.sendEmail(to, "Код подтверждения — ХозДача", body, true)
}

// SendPasswordResetCode отправляет код для сброса пароля
func (s *EmailService) SendPasswordResetCode(to, name, code string) error {
	greeting := name
	if greeting == "" {
		greeting = "здравствуйте"
	} else {
		greeting = "здравствуйте, " + name
	}
	body := `<p style="color:#444;font-size:15px;line-height:1.6;margin:0 0 8px">` + greeting + `!</p>` +
		`<p style="color:#555;font-size:14px;line-height:1.6;margin:0 0 20px">Для сброса пароля введите этот код на сайте:</p>` +
		codeDigits(code) +
		`<p style="color:#888;font-size:13px;text-align:center;margin:0">Код действителен 30 минут</p>`
	return s.sendEmail(to, "Сброс пароля — ХозДача", body, true)
}

// SendOrderConfirmation отправляет подтверждение заказа
func (s *EmailService) SendOrderConfirmation(to, name string, orderID int64, total float64) error {
	greeting := name
	if greeting == "" {
		greeting = "здравствуйте"
	} else {
		greeting = "здравствуйте, " + name
	}
	body := `<p style="color:#444;font-size:15px;line-height:1.6;margin:0 0 16px">` + greeting + `!</p>` +
		`<p style="color:#555;font-size:14px;line-height:1.6;margin:0 0 8px">Ваш заказ оформлен:</p>` +
		`<table cellpadding="0" cellspacing="0" style="background:#f7f9fc;border-radius:8px;padding:16px;width:100%%;margin-bottom:16px"><tr><td style="padding:8px 0;font-size:14px;color:#333">` +
		`<b>№ заказа:</b> ` + strconv.FormatInt(orderID, 10) + `</td></tr><tr><td style="padding:8px 0;font-size:14px;color:#333">` +
		`<b>Сумма:</b> ` + strconv.FormatFloat(total, 'f', 2, 64) + ` руб.</td></tr></table>` +
		`<p style="color:#888;font-size:13px;margin:0">Менеджер свяжется с вами для подтверждения.</p>`
	return s.sendEmail(to, "Заказ оформлен — ХозДача", body, true)
}

// sendEmail отправляет email через SMTPS. Если html=true, body интерпретируется как HTML.
func (s *EmailService) sendEmail(to, subject, body string, html bool) error {
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
	finalBody := body
	if html {
		finalBody = emailWrap(subject, body)
	}
	msg := buildMsg(from, to, subject, finalBody, html)

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
