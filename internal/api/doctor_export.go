package api

import (
	"net/http"
	"regexp"
	"strings"
)

const DoctorExportsPath = "/api/doctorchart/exports"

var exportID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// DoctorExportRoute grants access to the physician's own data even when
// their paid feature entitlement has expired.
func DoctorExportRoute(method, path string) bool {
	if method == http.MethodPost {
		return path == DoctorExportsPath
	}
	if method != http.MethodGet {
		return false
	}
	if path == DoctorExportsPath {
		return true
	}
	id, ok := strings.CutPrefix(path, DoctorExportsPath+"/")
	return ok && exportID.MatchString(strings.TrimSuffix(id, "/file"))
}
