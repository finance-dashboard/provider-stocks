package internal

import (
	"time"

	tinkoff "github.com/TinkoffCreditSystems/invest-openapi-go-sdk"
)

type Currency = tinkoff.Currency

type Cost struct {
	Low      float64  `json:"low"`
	High     float64  `json:"high"`
	Currency Currency `json:"currency"`
}

type Update struct {
	Time   time.Time `json:"time"`
	Name   string    `json:"name"`
	Ticker string    `json:"ticker"`
	Cost   Cost      `json:"cost"`
}
