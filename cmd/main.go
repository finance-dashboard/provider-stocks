package main

import (
	"context"
	"encoding/json"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"finance-dashboard-provider-stocks/internal"

	tinkoff "github.com/TinkoffCreditSystems/invest-openapi-go-sdk"
	"github.com/gorilla/websocket"
)

var log = stdlog.New(os.Stdout, "[main] ", stdlog.LstdFlags|stdlog.Lshortfile|stdlog.Lmsgprefix)

func main() {
	upgrader := websocket.Upgrader{
		EnableCompression: true,
		HandshakeTimeout:  time.Second * 15,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
			w.WriteHeader(status)
			log.Printf("error in ws: %+v", reason)
		},
	}

	updates := make(chan internal.Update)
	subscribers := internal.NewSubscriberList()

	go startBroadcast(subscribers, updates)

	token := os.Getenv("TOKEN")
	rest := tinkoff.NewSandboxRestClient(token)
	stream, err := tinkoff.NewStreamingClient(log, token)
	if err != nil {
		log.Fatal(err)
	}

	tickers := strings.Split(os.Getenv("TICKERS"), ";")
	instrumentsByFIGI, _ := collectInstrumentsInfo(rest, tickers)

	cache, err := populateCache(rest, instrumentsByFIGI)
	if err != nil {
		log.Fatal(err)
	}

	cacheSubscriber := subscribers.Subscribe()
	defer subscribers.Unsubscribe(cacheSubscriber)

	go func() {
		for update := range cacheSubscriber.Updates {
			cache.Set(update)
		}
	}()

	go readEventsFromAPI(stream, instrumentsByFIGI, updates)

	for figi := range instrumentsByFIGI {
		if err := stream.SubscribeOrderbook(figi, 1, ""); err != nil {
			log.Fatal(err)
		}
	}

	go func() {
		for {
			time.Sleep(time.Second * 15)
			log.Printf("goroutines: %d, subscribers: %d", runtime.NumGoroutine(), subscribers.Len())
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/stocks", handleIncomingReq(upgrader, subscribers, cache))

	server := http.Server{Addr: ":8081", Handler: mux}

	log.Printf("listening on port 8081")
	log.Fatal(server.ListenAndServe())
}

func populateCache(rest *tinkoff.SandboxRestClient, instrumentsByFIGI InstrumentMap) (*internal.Cache, error) {
	cache := internal.NewCache()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for figi, instrument := range instrumentsByFIGI {
		candles, err := rest.Candles(ctx, time.Now().Add(-24*7*time.Hour), time.Now(), tinkoff.CandleInterval1Hour, figi)
		if err != nil {
			return nil, fmt.Errorf("error getting instrument info: %v", err)
		}

		if len(candles) == 0 {
			log.Printf("instrument %s has no candles", instrument.Ticker)
			continue
		}

		lastCandle := candles[len(candles)-1]

		cache.Set(internal.Update{
			Time:   lastCandle.TS,
			Name:   instrument.Name,
			Ticker: instrument.Ticker,
			Cost: internal.Cost{
				Low:      lastCandle.ClosePrice,
				High:     lastCandle.ClosePrice,
				Currency: instrument.Currency,
			},
		})
	}

	return cache, nil
}

func readEventsFromAPI(stream *tinkoff.StreamingClient, instrumentsByFIGI InstrumentMap, updates chan internal.Update) {
	if err := stream.RunReadLoop(func(event interface{}) error {
		switch event := event.(type) {
		case tinkoff.OrderBookEvent:
			instrument := instrumentsByFIGI[event.OrderBook.FIGI]

			if len(event.OrderBook.Bids) == 0 || len(event.OrderBook.Asks) == 0 {
				log.Printf("no bids/asks for ticker %s (is exchange closed for ticker?)", instrument.Ticker)
				return nil
			}

			updates <- internal.Update{
				Time:   event.Time,
				Name:   instrument.Name,
				Ticker: instrument.Ticker,
				Cost: internal.Cost{
					Low:      event.OrderBook.Bids[0][0],
					High:     event.OrderBook.Asks[0][0],
					Currency: instrument.Currency,
				},
			}
		default:
			return fmt.Errorf("unknown event %v", event)
		}

		return nil
	}); err != nil {
		log.Fatal(err)
	}
}

func handleIncomingReq(upgrader websocket.Upgrader, subscribers *internal.SubscriberList, cache *internal.Cache) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("client subscribed")

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print(err)
			return
		}

		defer func() {
			_ = conn.Close()
		}()

		for _, update := range cache.Snapshot() {
			if ok := sendUpdate(conn, update); !ok {
				return
			}
		}

		subscriber := subscribers.Subscribe()
		defer subscribers.Unsubscribe(subscriber)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			for {
				select {
				case update := <-subscriber.Updates:
					if ok := sendUpdate(conn, update); !ok {
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}()

		for {
			_ = conn.SetReadDeadline(time.Now().Add(time.Minute * 5))
			code, message, err := conn.ReadMessage()
			if err != nil {
				log.Print(err)
				return
			}

			if code == websocket.CloseMessage {
				log.Printf("websocket closing")
				return
			}

			log.Printf("received unexpected message %+v", message)
		}
	}
}

func sendUpdate(conn *websocket.Conn, update internal.Update) bool {
	b, err := json.Marshal(update)
	if err != nil {
		log.Print(err)
		return false
	}

	_ = conn.SetWriteDeadline(time.Now().Add(time.Second * 15))
	_ = conn.WriteMessage(websocket.TextMessage, b)

	return true
}

type InstrumentMap = map[string]tinkoff.Instrument

func collectInstrumentsInfo(rest *tinkoff.SandboxRestClient, tickers []string) (byFIGI InstrumentMap, byTickers InstrumentMap) {
	byFIGI = make(InstrumentMap)
	byTickers = make(InstrumentMap)

	for _, ticker := range tickers {
		res, err := rest.InstrumentByTicker(context.Background(), ticker)
		if err != nil {
			log.Fatal(err)
		}

		if len(res) != 1 {
			log.Fatalf("number of instruments with ticker %s is %d", ticker, len(res))
		}

		instrument := res[0]

		byFIGI[instrument.FIGI] = instrument
		byTickers[instrument.Ticker] = instrument
	}

	return
}

func startBroadcast(subscribers *internal.SubscriberList, updates <-chan internal.Update) {
	for {
		update := <-updates

		snapshot := subscribers.Snapshot()
		for _, item := range snapshot {
			item.Updates <- update
		}
	}
}
