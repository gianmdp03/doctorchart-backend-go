package billing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
)

const doctorAvatarMaxBytes = 200_000

var (
	ErrDoctorAvatarMissing     = errors.New("doctor avatar missing")
	errDoctorAvatarUnavailable = errors.New("doctor avatar unavailable")
)

func (e *Entitlements) DoctorAvatar(ctx context.Context, owner string) ([]byte, error) {
	if e.secret == "" {
		return nil, errDoctorAvatarUnavailable
	}
	endpoint := e.base + "/internal/doctorchart/owners/" + url.PathEscape(owner) + "/avatar"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errDoctorAvatarUnavailable
	}
	req.Header.Set("X-Internal-Secret", e.secret)
	res, err := e.client.Do(req)
	if err != nil {
		return nil, errDoctorAvatarUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrDoctorAvatarMissing
	}
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/webp" {
		return nil, errDoctorAvatarUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, doctorAvatarMaxBytes+1))
	if err != nil || len(data) > doctorAvatarMaxBytes || http.DetectContentType(data) != "image/webp" {
		return nil, errDoctorAvatarUnavailable
	}
	return data, nil
}
