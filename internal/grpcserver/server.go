package grpcserver

import (
	"fmt"
	"time"

	"finance-dashboard-provider-stocks/api"

	tinkoff "github.com/TinkoffCreditSystems/invest-openapi-go-sdk"
)

type Instruments = map[string]tinkoff.Instrument

type CurrencyProvider struct {
	rest        *tinkoff.RestClient
	instruments Instruments
	api.UnsafeCurrencyProviderServer
}

func New(rest *tinkoff.RestClient, instruments Instruments) api.CurrencyProviderServer {
	return CurrencyProvider{
		rest:                         rest,
		instruments:                  instruments,
		UnsafeCurrencyProviderServer: nil,
	}
}

func (s CurrencyProvider) GetCurrency(slice *api.TimeSlice, server api.CurrencyProvider_GetCurrencyServer) error {
	from, err := time.Parse(time.RFC3339, slice.Start)
	if err != nil {
		return err
	}

	end, err := time.Parse(time.RFC3339, slice.End)
	if err != nil {
		return err
	}

	instrument, ok := s.instruments[slice.CurrencyCode]
	if !ok {
		return fmt.Errorf("no stock %q found", slice.CurrencyCode)
	}

	values, err := s.rest.Candles(server.Context(), from, end, tinkoff.CandleInterval1Day, instrument.FIGI)
	if err != nil {
		return err
	}

	for _, value := range values {
		if err := server.Send(&api.Value{Value: float32(value.ClosePrice)}); err != nil {
			return err
		}
	}

	return nil
}
