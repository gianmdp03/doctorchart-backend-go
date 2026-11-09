package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type smtpSender struct{ config Config }

func NewSMTP(c Config) (Sender, error) {
	from, err := mail.ParseAddress(c.From)
	if err != nil || from.Address != c.From || strings.ContainsAny(c.Host, "\r\n/: ") || c.Host == "" || c.Port < 1 || c.Port > 65535 || c.TLSMode != "starttls" && c.TLSMode != "tls" || c.Username == "" && c.Password != "" {
		return nil, errors.New("invalid SMTP configuration")
	}
	return &smtpSender{config: c}, nil
}
func (s *smtpSender) Send(ctx context.Context, to string, body []byte) error {
	address, err := mail.ParseAddress(to)
	if err != nil || address.Address != to {
		return errors.New("invalid SMTP recipient")
	}
	c := s.config
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	tlsConfig := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.TLSMode == "tls" {
		secure := tls.Client(conn, tlsConfig)
		if err = secure.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if c.TLSMode == "starttls" {
		if available, _ := client.Extension("STARTTLS"); !available {
			return errors.New("SMTP TLS unavailable")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if c.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return err
		}
	}
	if err = client.Mail(c.From); err != nil {
		return err
	}
	if err = client.Rcpt(address.Address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(body); err != nil {
		_ = writer.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	// DATA's positive completion is delivery acceptance; QUIT failure cannot undo it.
	_ = client.Quit()
	return nil
}
func (s *Service) render(kind, key string, m Message) ([]byte, error) {
	if m.Date.IsZero() {
		return nil, errors.New("missing notification date")
	}
	recipient, err := mail.ParseAddress(m.To)
	if err != nil || recipient.Address != m.To {
		return nil, errors.New("invalid recipient")
	}
	subject, text := "", ""
	if kind == "STAFF_INVITATION" {
		subject = "Invitación a la agenda de Doctor Chart"
		text = "Un médico te invitó a gestionar su agenda.\nAbrí " + s.origin + "/staff/activate e ingresá este código privado:\n\n" + m.Code + "\n\nVence el " + m.Expires.UTC().Format("02/01/2006 15:04") + " UTC. Si ya tenés cuenta local, usá tu contraseña actual. No compartas este código ni tu contraseña."
	} else if kind == "STAFF_RECOVERY" {
		subject = "Recuperación de acceso a Doctor Chart"
		text = "Recibimos una solicitud para recuperar tu acceso local de secretaría.\nAbrí " + s.origin + "/staff/recover y elegí recuperación por email. Ingresá este código:\n\n" + m.Code + "\n\nVence en 15 minutos. Si no lo solicitaste, ignorá este correo: tu contraseña y sesiones no fueron modificadas. Nunca compartas contraseñas ni tu código personal de recuperación."
	} else {
		switch kind {
		case "BOOKING_CONFIRMED":
			subject = "Tu turno está confirmado"
		case "BOOKING_CANCELLED":
			subject = "Tu turno fue cancelado"
		case "BOOKING_RESCHEDULED":
			subject = "Tu turno fue reprogramado"
		case "BOOKING_REMINDER":
			subject = "Recordatorio de tu turno"
		default:
			return nil, errors.New("unknown notification kind")
		}
		zone, err := time.LoadLocation(m.Timezone)
		if err != nil {
			return nil, errors.New("invalid notification timezone")
		}
		text = subject + ".\n\nProfesional: " + m.Doctor + "\nConsultorio: " + m.Clinic + "\nFecha y hora: " + m.Start.In(zone).Format("02/01/2006 15:04") + " (hora local del consultorio)\n"
		if m.Address != "" {
			text += "Dirección pública: " + m.Address + "\n"
		}
		text += "Referencia: " + m.Reference + "\n"
		if m.Code != "" {
			text += "\nPara consultar el estado actualizado, abrí " + s.origin + "/appointments/confirmation e ingresá tu código privado:\n\n" + m.Code + "\n\nEl código vence siete días después del turno. No lo compartas.\n"
		}
	}
	text += "\n\nDoctor Chart · Aviso administrativo. Este correo no contiene información clínica.\n"
	var body bytes.Buffer
	fmt.Fprintf(&body, "From: Doctor Chart <%s>\r\nTo: <%s>\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <dc-%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n", s.from, recipient.Address, mime.QEncoding.Encode("utf-8", subject), m.Date.UTC().Format(time.RFC1123Z), Hash(key), strings.Split(s.from, "@")[1])
	// Base64 protects SMTP line length and preserves Spanish text end to end.
	encoded := base64Text([]byte(strings.ReplaceAll(text, "\n", "\r\n")))
	body.WriteString(encoded)
	return body.Bytes(), nil
}
