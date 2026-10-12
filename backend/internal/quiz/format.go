package quiz

import "time"

const timeFormat = "2006-01-02T15:04:05Z07:00"

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(timeFormat)
}
