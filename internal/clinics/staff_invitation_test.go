package clinics

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStaffInvitationLinkInspectionAndOptionalPhone(t *testing.T) {
	f, c, _ := staffFixture(t)
	body := map[string]any{"email": "Assistant@Example.Invalid", "name": "Ana", "phone": "+5491123456789", "clinicIds": []string{c.ID}, "permissions": []string{"view", "create"}}
	w := f.request("POST", "/secretaries/invitations", owner, jsonString(body))
	assertCode(t, w, 201, "")
	var result struct {
		ID, InvitationCode string
		ExpiresAt          time.Time
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if time.Until(result.ExpiresAt) < 5*time.Hour+59*time.Minute || time.Until(result.ExpiresAt) > 6*time.Hour {
		t.Fatal("unexpected invitation expiry", result.ExpiresAt)
	}
	w = f.request("GET", "/secretaries", owner, "")
	assertCode(t, w, 200, "")
	var list struct{ Items []staffAccess }
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Phone != "+5491123456789" || list.Items[0].Email != "assistant@example.invalid" {
		t.Fatalf("contact not persisted: %s", w.Body)
	}
	if _, err := f.pool.Exec(t.Context(), "UPDATE staff_access SET phone='invalid' WHERE id=$1", result.ID); err == nil {
		t.Fatal("database accepted malformed phone")
	}
	for range 2 {
		w = staffReq(f, staffTestClient{}, "POST", "/invitation", map[string]string{"invitationCode": result.InvitationCode})
		assertCode(t, w, 200, "")
		var info struct {
			Email, Name     string
			ExistingAccount bool
		}
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		if info.Email != "assistant@example.invalid" || info.Name != "Ana" || info.ExistingAccount {
			t.Fatalf("invalid link information: %s", w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store, private" || w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("invitation response may leak")
		}
	}
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/activate", map[string]string{"invitationCode": result.InvitationCode, "email": "other@example.invalid", "password": staffTestPassword}), 401, "INVALID_CREDENTIALS")
	staffActivateTest(t, f, result.InvitationCode)
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/invitation", map[string]string{"invitationCode": result.InvitationCode}), 401, "INVALID_CREDENTIALS")
}

func TestStaffInvitationLinkRenewalExpiryRevocationAndContacts(t *testing.T) {
	f, c, _ := staffFixture(t)
	for _, phone := range []string{"5491123456789", "+0123456789", "+54911<script>"} {
		assertCode(t, f.request("POST", "/secretaries/invitations", owner, jsonString(map[string]any{"name": "Ana", "email": "assistant@example.invalid", "phone": phone, "clinicIds": []string{c.ID}, "permissions": []string{"view"}})), 400, "INVALID_REQUEST")
	}
	assertCode(t, f.request("POST", "/secretaries/invitations", owner, jsonString(map[string]any{"name": "Ana", "clinicIds": []string{c.ID}, "permissions": []string{"view"}})), 400, "INVALID_REQUEST")
	id, old := staffInvite(t, f, owner, c, []string{"view"})
	_, code := staffInvite(t, f, owner, c, []string{"view"})
	inspect := func(code string, status int) {
		t.Helper()
		assertCode(t, staffReq(f, staffTestClient{}, "POST", "/invitation", map[string]string{"invitationCode": code}), status, "")
	}
	inspect(old, 401)
	inspect(code, 200)
	if _, err := f.pool.Exec(t.Context(), "UPDATE staff_invitations SET expires_at=now()-interval '1 second' WHERE token_hash=$1", staffHash(code)); err != nil {
		t.Fatal(err)
	}
	inspect(code, 401)
	_, code = staffInvite(t, f, owner, c, []string{"view"})
	assertCode(t, f.request("DELETE", "/secretaries/"+id, owner, ""), 204, "")
	inspect(code, 401)
	inspect(staffToken(), 401)
}

func TestStaffInvitationLinkRecognizesExistingAccount(t *testing.T) {
	f, c, _ := staffFixture(t)
	_, code := staffInvite(t, f, owner, c, []string{"view"})
	staffActivateTest(t, f, code)
	otherClinic := clinic(t, f.request("POST", "/clinics", other, createBody))
	_, code = staffInvite(t, f, other, otherClinic, []string{"view"})
	w := staffReq(f, staffTestClient{}, "POST", "/invitation", map[string]string{"invitationCode": code})
	assertCode(t, w, 200, "")
	var info struct{ ExistingAccount bool }
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !info.ExistingAccount {
		t.Fatal("existing secretary must use current password")
	}
}
