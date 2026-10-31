package clinics

import "net/http"

func (s *Service) publicRoutes(mux *http.ServeMux, wrap func(func(http.ResponseWriter, *http.Request, string) error) http.HandlerFunc) {
	mux.HandleFunc("GET /api/doctorchart/booking-publication", wrap(s.getPublication))
	mux.HandleFunc("PUT /api/doctorchart/booking-publication", wrap(s.putPublication))
	mux.HandleFunc("GET /api/doctorchart/bookings/{id}/public-details", wrap(s.publicBookingDetails))
	mux.HandleFunc("GET /api/doctorchart/public/appointments/doctors", s.publicWrap("read", 120, s.publicDirectory))
	mux.HandleFunc("GET /api/doctorchart/public/appointments/doctors/{publicId}", s.publicWrap("read", 120, s.publicProfile))
	mux.HandleFunc("GET /api/doctorchart/public/appointments/doctors/{publicId}/clinics/{publicClinicId}/availability", s.publicWrap("read", 120, s.publicAvailability))
	mux.HandleFunc("POST /api/doctorchart/public/appointments/challenge", s.publicWrap("challenge", 20, s.challenge))
	mux.HandleFunc("POST /api/doctorchart/public/appointments/doctors/{publicId}/bookings", s.publicWrap("create", 10, s.publicCreate))
}
