package internal

import (
	"time"
)

type Cost struct {
	Low  float64 `json:"low"`
	High float64 `json:"high"`
}

type Update struct {
	Time   time.Time `json:"time"`
	Name   string    `json:"name"`
	Ticker string    `json:"ticker"`
	Cost   Cost      `json:"cost"`
}
