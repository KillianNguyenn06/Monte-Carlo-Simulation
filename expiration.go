package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	_ "time/tzdata"
)

const expirationLayout = "2006-01-02 15:04"

// Offset timestamps identify an exact instant. Offset-free input explicitly uses
// New York wall time; ambiguous/nonexistent daylight-saving times need an offset.
func parseExpiration(value string, location *time.Location) (time.Time, error) {
	if instant, err := time.Parse(time.RFC3339, value); err == nil {
		return instant.In(location), nil
	}
	instant, err := time.ParseInLocation(expirationLayout, value, location)
	if err != nil || instant.Format(expirationLayout) != value {
		return time.Time{}, fmt.Errorf("enter YYYY-MM-DD HH:MM in New York time, or RFC3339 with an offset")
	}
	for _, shift := range []time.Duration{-time.Hour, time.Hour} {
		if instant.Add(shift).Format(expirationLayout) == value {
			return time.Time{}, fmt.Errorf("ambiguous daylight-saving time; enter RFC3339 with an explicit offset")
		}
	}
	return instant, nil
}

func remainingDays(valuation, expiration time.Time) float64 {
	if !expiration.After(valuation) {
		return 0
	}
	return expiration.Sub(valuation).Hours() / 24
}

func collectExpiration(reader *bufio.Reader, writer io.Writer, sim *MonteCarlo) error {
	days, err := promptFloat(reader, writer, "Target days to expiration (DTE): ", nil, func(v float64) bool { return isFinite(v) && v >= 0 && v == math.Trunc(v) })
	if err != nil {
		return err
	}
	if days > 0 {
		sim.ExpirationDays = expirationDate(days)
		sim.Expirations = nil // A whole-day scenario does not claim an exact contract timestamp.
		return nil
	}
	return collectSameDayExpiration(reader, writer, sim)
}

func collectSameDayExpiration(reader *bufio.Reader, writer io.Writer, sim *MonteCarlo) error {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		return err
	}
	if sim.ValuationTime.IsZero() {
		return fmt.Errorf("valuation timestamp is required")
	}
	fmt.Fprintf(writer, "Valuation time: %s\n", sim.ValuationTime.In(location).Format(time.RFC3339))
	for {
		value, err := promptRequired(reader, writer, "Today’s expiration time (HH:MM, New York time): ")
		if err != nil {
			return err
		}
		if len(value) == 5 && strings.Contains(value, ":") {
			value = sim.ValuationTime.In(location).Format("2006-01-02") + " " + value
		}
		first, err := parseExpiration(value, location)
		if err != nil {
			fmt.Fprintln(writer, err)
			continue
		}
		if first.Format("2006-01-02") != sim.ValuationTime.In(location).Format("2006-01-02") {
			fmt.Fprintln(writer, "0 DTE requires today's expiration in New York time.")
			continue
		}
		if first.Before(sim.ValuationTime) {
			fmt.Fprintln(writer, "Expiration precedes this quote snapshot. Enter a current/future expiration; historical settlement needs its own settlement price.")
			continue
		}
		sim.Expirations = make([]time.Time, 5)
		sim.ExpirationDays = make([]float64, 5)
		for i := range sim.Expirations {
			// Weekly synthetic expirations preserve local wall time across DST changes.
			sim.Expirations[i] = first.AddDate(0, 0, 7*i)
			sim.ExpirationDays[i] = remainingDays(sim.ValuationTime, sim.Expirations[i])
		}
		return nil
	}
}

func formatDTE(days float64) string {
	if days == 0 {
		return "0d"
	}
	if days < 1 {
		return fmt.Sprintf("%.2fh", days*24)
	}
	if days == math.Trunc(days) {
		return fmt.Sprintf("%.0fd", days)
	}
	return fmt.Sprintf("%.3fd", days)
}

func timestampString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func (sim *MonteCarlo) expirationString(column int) string {
	if column >= len(sim.Expirations) {
		return ""
	}
	return timestampString(sim.Expirations[column])
}
