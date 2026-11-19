package notifications

import (
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/mailclient"
)

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
		text = subject + ".\nFecha y hora: " + m.Start.In(zone).Format("02/01/2006 a las 15:04 (MST)") + "."
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
