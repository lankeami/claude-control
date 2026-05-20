package db

import "time"

// FiveHourWindow returns the start and end of the 5-hour window containing the given timestamp.
// Windows are: 00:00-05:00, 05:00-10:00, 10:00-15:00, 15:00-20:00, 20:00-25:00 (next day)
// Example: 12:30 UTC falls in 10:00-15:00 window
func FiveHourWindow(ts time.Time) (start, end time.Time) {
	ts = ts.UTC()
	hour := ts.Hour()
	windowNum := hour / 5 // 0-4 for hours 0-23

	start = time.Date(ts.Year(), ts.Month(), ts.Day(), windowNum*5, 0, 0, 0, time.UTC)
	end = start.Add(5 * time.Hour)

	return start, end
}

// SevenDayWindow returns the start and end of the 7-day window (Sunday-Sunday UTC) containing the given timestamp.
// Sunday 00:00 UTC to next Sunday 00:00 UTC
// Example: Tuesday 2026-05-19 falls in Sunday 2026-05-17 to Sunday 2026-05-24
func SevenDayWindow(ts time.Time) (start, end time.Time) {
	ts = ts.UTC()

	// Find the most recent Sunday at 00:00 UTC
	daysUntilSunday := int(ts.Weekday()) // 0=Sunday, 1=Monday, ..., 6=Saturday
	start = time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
	start = start.AddDate(0, 0, -daysUntilSunday)

	// Next Sunday is 7 days later
	end = start.AddDate(0, 0, 7)

	return start, end
}
