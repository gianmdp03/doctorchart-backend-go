package clinical

import "time"

// Date ranges are (after, through], shared by badges and directory SQL filters.
// Each valid birth date belongs to exactly one age category.
type pediatricAgeRange struct{ after, through *time.Time }

func yearsBefore(today time.Time, years int) time.Time {
	year := today.Year() - years
	day := min(today.Day(), time.Date(year, today.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day())
	return time.Date(year, today.Month(), day, 0, 0, 0, 0, time.UTC)
}

func pediatricRange(filter string, today time.Time) (pediatricAgeRange, bool) {
	neonatal := today.AddDate(0, 0, -28)
	one, five, ten, twenty := yearsBefore(today, 1), yearsBefore(today, 5), yearsBefore(today, 10), yearsBefore(today, 20)
	switch filter {
	case "NEONATAL":
		return pediatricAgeRange{&neonatal, &today}, true
	case "INFANT":
		return pediatricAgeRange{&one, &neonatal}, true
	case "EARLY_CHILDHOOD":
		return pediatricAgeRange{&five, &one}, true
	case "CHILD":
		return pediatricAgeRange{&ten, &five}, true
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
