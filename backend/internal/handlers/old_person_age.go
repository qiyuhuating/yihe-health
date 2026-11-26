package handlers

import (
	"time"
)

func ageOn(birth, at time.Time) int {
	age := at.Year() - birth.Year()
	if at.Month() < birth.Month() || (at.Month() == birth.Month() && at.Day() < birth.Day()) {
		age--
	}
	return age
}

func currentPersonAge(birth *time.Time, stored *int, at time.Time) *int {
	if birth == nil {
		return stored
	}
	age := ageOn(*birth, at)
	if birth.Year() < 1 || age < 0 {
		return nil
	}
	return &age
}
