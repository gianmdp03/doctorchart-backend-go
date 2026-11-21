package notifications

import (
	"bytes"
	"errors"
	"html/template"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/mailclient"
)

var bookingChangeHTML = template.Must(template.New("booking-change").Parse(`<!doctype html>
<html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="margin:0;background:#f3f6f8;font-family:Arial,sans-serif;color:#19313f;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:#f3f6f8;"><tr><td align="center" style="padding:24px 12px;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:560px;background:#ffffff;border:1px solid #dce5ea;border-radius:16px;text-align:left;">
<tr><td style="padding:28px 28px 0;color:#08666a;font-size:16px;font-weight:bold;">DoctorChart</td></tr>
<tr><td style="padding:18px 28px 28px;">
<h1 style="margin:0 0 16px;font-size:25px;line-height:1.25;color:#19313f;">{{.Title}}</h1>
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:#e5f4f1;border-radius:10px;"><tr><td style="padding:15px 16px;">
<div style="color:#08666a;font-size:13px;font-weight:bold;">{{.DateLabel}}</div>
<div style="margin-top:5px;color:#19313f;font-size:17px;font-weight:bold;line-height:1.4;">{{.When}}</div>
</td></tr></table>
{{if .ReasonLines}}<h2 style="margin:22px 0 8px;font-size:16px;">{{.ReasonLabel}}</h2>
<div style="font-size:15px;line-height:1.55;overflow-wrap:anywhere;">{{range .ReasonLines}}{{.}}<br>{{end}}</div>{{end}}
<p style="margin:22px 0 15px;color:#4f6471;font-size:15px;line-height:1.55;">{{.Instructions}}</p>
<table role="presentation" cellspacing="0" cellpadding="0"><tr><td bgcolor="#08666a" style="border-radius:9px;">
<a href="{{.URL}}" style="display:inline-block;padding:13px 20px;border:1px solid #08666a;border-radius:9px;color:#ffffff;text-decoration:none;font-size:15px;font-weight:bold;">{{.Action}}</a>
</td></tr></table>
</td></tr>
<tr><td style="padding:16px 28px;border-top:1px solid #dce5ea;color:#4f6471;font-size:12px;line-height:1.45;">DoctorChart · Aviso administrativo de tu turno.</td></tr>
</table>
</td></tr></table>
</body></html>`))

type bookingChangeEmail struct {
	Title, DateLabel, When, ReasonLabel, Instructions, URL, Action string
	ReasonLines                                                    []string
}

func (s *Service) renderBookingChange(subject, key, recipient, when string, change bookingChange, cancelled bool) (mailclient.Message, error) {
	view := bookingChangeEmail{
		Title: subject, DateLabel: "Nueva fecha y hora", When: when,
		ReasonLabel:  "Motivo de la reprogramación",
		Instructions: "Podés consultar el turno actualizado y gestionar la reserva en Mis turnos.",
		Action:       "Ver mis turnos", URL: s.origin + "/appointments/confirmation",
	}
	if cancelled {
		view.DateLabel = "Fecha y hora del turno cancelado"
		view.ReasonLabel = "Motivo de la cancelación"
		view.Instructions = "Para sacar un turno nuevo, entrá a DoctorChart, revisá los horarios disponibles y elegí el que te quede mejor."
		view.Action = "Buscar un nuevo turno"
		view.URL = s.origin + "/appointments"
		if change.PublicID != "" {
			view.URL += "/" + url.PathEscape(change.PublicID)
		}
	}
	text := subject + ".\n\n" + view.DateLabel + "\n" + view.When
	if change.Reason != "" {
		view.ReasonLines = strings.Split(change.Reason, "\n")
		text += "\n\n" + view.ReasonLabel + "\n" + change.Reason
	}
	text += "\n\n" + view.Instructions + "\n" + view.URL + "\n\nDoctorChart · Aviso administrativo de tu turno.\n"
	var html bytes.Buffer
	if err := bookingChangeHTML.Execute(&html, view); err != nil {
		return mailclient.Message{}, err
	}
	return mailclient.Message{
		ID: "doctorchart:" + Hash(key), Category: "appointments", To: recipient,
		Subject: subject, Text: text, HTML: html.String(),
	}, nil
}

func (s *Service) render(kind, key string, m Message) (mailclient.Message, error) {
	if m.Date.IsZero() {
		return mailclient.Message{}, errors.New("missing notification date")
	}
	recipient, err := mail.ParseAddress(m.To)
	if err != nil || recipient.Address != m.To {
		return mailclient.Message{}, errors.New("invalid recipient")
	}
	subject, text := "", ""
	if kind == "STAFF_INVITATION" {
		link, err := url.Parse(m.StaffOrigin)
		if err != nil || link.Scheme != "https" || link.Host == "" || link.User != nil ||
			link.Path != "" || link.RawQuery != "" || link.ForceQuery || link.Fragment != "" || len(m.Code) != 43 {
			return mailclient.Message{}, errors.New("invalid staff invitation")
		}
		link.Path = "/staff/activate"
		link.Fragment = "invite=" + m.Code
		subject = "Invitación a la agenda de DoctorChart"
		text = "Un profesional te invitó a gestionar su agenda en DoctorChart.\n\n" +
			"Aceptá la invitación desde este enlace privado:\n" + link.String() +
			"\n\nEl enlace es de un solo uso y vence el " + m.Expires.UTC().Format("02/01/2006 15:04") +
			" UTC. Si ya tenés cuenta de secretaría, usá tu contraseña actual."
	} else if kind == "BOOKING_PORTAL_CODE" {
		subject = "Tu código de DoctorChart"
		text = "Tu código para acceder a Mis turnos es:\n\n" + m.Code + "\n\nVence en 10 minutos. Si no lo pediste, ignorá este correo."
	} else if kind == "STAFF_RECOVERY" {
		subject = "Recuperación de acceso a Doctor Chart"
		text = "Recibimos una solicitud para recuperar tu acceso local de secretaría.\nAbrí " + s.origin + "/staff/recover y elegí recuperación por email. Ingresá este código:\n\n" + m.Code + "\n\nVence en 15 minutos. Si no lo solicitaste, ignorá este correo: tu contraseña y sesiones no fueron modificadas. Nunca compartas contraseñas ni tu código personal de recuperación."
	} else {
		switch kind {
		case "BOOKING_CONFIRMED":
			subject = "Tu turno está confirmado"
		case "BOOKING_RESCHEDULED":
			subject = "Tu turno fue reprogramado"
		case "BOOKING_CANCELLED":
			subject = "Tu turno fue cancelado"
		case "BOOKING_REMINDER":
			subject = "Recordatorio de tu turno"
		default:
			return mailclient.Message{}, errors.New("unknown notification kind")
		}
		zone, err := time.LoadLocation(m.Timezone)
		if err != nil {
			return mailclient.Message{}, err
		}
		when := m.Start.In(zone).Format("02/01/2006 a las 15:04 (MST)")
		if m.Change != nil && (kind == "BOOKING_CANCELLED" || kind == "BOOKING_RESCHEDULED") {
			return s.renderBookingChange(subject, key, recipient.Address, when, *m.Change, kind == "BOOKING_CANCELLED")
		}
		text = subject + ".\nFecha y hora: " + when + "."
	}
	if strings.HasPrefix(kind, "BOOKING_") {
		text += "\n\n¿Necesitás reprogramar, cancelar o consultar un turno? Entrá a DoctorChart → Mis turnos. Ingresá tu DNI y el correo de la reserva, y usá el código que te llega por email. Si ya verificaste este navegador, podés entrar directamente durante 48 horas."
	}
	text += "\n\nDoctor Chart · Aviso administrativo. Este correo no contiene información clínica.\n"
	category := ""
	switch {
	case strings.HasPrefix(kind, "BOOKING_"):
		category = "appointments"
	case kind == "STAFF_RECOVERY":
		category = "security"
	}
	return mailclient.Message{
		ID: "doctorchart:" + Hash(key), Category: category, To: recipient.Address, Subject: subject, Text: text,
	}, nil
}
