// growth-reference exports the verified scientific catalogue for offline visual
// review/testing. It does not read patient data or connect to a database.
package main

import (
	"encoding/json"
	"github.com/syntah/doctorchart-backend-go/internal/growth"
	"log"
	"os"
)

func main() {
	r, err := growth.Load()
	if err != nil {
		log.Fatal(err)
	}
	charts := []growth.ReferenceChart{}
	for _, d := range growth.Catalog() {
		charts = append(charts, r.Chart(d))
	}
	if err := json.NewEncoder(os.Stdout).Encode(charts); err != nil {
		log.Fatal(err)
	}
}
