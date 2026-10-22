package clinical

import "time"

// Date ranges are (after, through], shared by badges and directory SQL filters.
// Early childhood is operationalized as completed ages 0..8 (before birthday 9).
// It overlaps infancy and childhood; choose the most specific badge first.
type pediatricAgeRange struct{ after, through *time.Time }

func yearsBefore(today time.Time, years int) time.Time {
	year := today.Year() - years
	day := min(today.Day(), time.Date(year, today.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day())
	return time.Date(year, today.Month(), day, 0, 0, 0, 0, time.UTC)
}

func pediatricRange(filter string, today time.Time) (pediatricAgeRange, bool) {
	neonatal := today.AddDate(0, 0, -28)
	one, nine, ten, twenty := yearsBefore(today, 1), yearsBefore(today, 9), yearsBefore(today, 10), yearsBefore(today, 20)
	switch filter {
	case "NEONATAL":
		return pediatricAgeRange{&neonatal, &today}, true
	case "INFANT":
		return pediatricAgeRange{&one, &neonatal}, true
	case "EARLY_CHILDHOOD":
		return pediatricAgeRange{&nine, &today}, true
	case "CHILD":
		return pediatricAgeRange{&ten, &one}, true
	case "ADOLESCENT":
		return pediatricAgeRange{&twenty, &ten}, true
	case "ADULT":
		return pediatricAgeRange{through: &twenty}, true
	default:
		return pediatricAgeRange{}, false
	}
}

func (r pediatricAgeRange) contains(birth time.Time) bool {
	return (r.after == nil || birth.After(*r.after)) && (r.through == nil || !birth.After(*r.through))
}

func pediatricAgeGroup(birth, today time.Time) string {
	if birth.After(today) {
		return "UNKNOWN"
	}
	for _, group := range []string{"NEONATAL", "INFANT", "EARLY_CHILDHOOD", "CHILD", "ADOLESCENT", "ADULT"} {
		bounds, _ := pediatricRange(group, today)
		if bounds.contains(birth) {
			return group
		}
	}
	return "UNKNOWN"
}
