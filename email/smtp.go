package email

import (
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"epic_lab_reporter/config"
)

// Send sends an HTML email via Gmail STARTTLS (port 587).
// The subject includes today's date so each daily email is distinct in the inbox.
func Send(cfg *config.Config, htmlBody string) error {
	subject := fmt.Sprintf("Epic Lab Report — %s", time.Now().Format("02 Jan 2006"))

	// RFC 2822 message with MIME headers for HTML content.
	msg := strings.Join([]string{
		"From: " + cfg.SMTPFrom,
		"To: " + cfg.SMTPTo,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		htmlBody,
	}, "\r\n")

	auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)

	addr := cfg.SMTPHost + ":" + cfg.SMTPPort
	if err := smtp.SendMail(addr, auth, cfg.SMTPUser, []string{cfg.SMTPTo}, []byte(msg)); err != nil {
		return fmt.Errorf("smtp.SendMail: %w", err)
	}
	return nil
}
