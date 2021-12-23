package main

import (
	"context"
	"encoding/json"
	"fmt"
	stdlog "log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"finance-dashboard-provider-stocks/api"
	"finance-dashboard-provider-stocks/internal/cache"
	"finance-dashboard-provider-stocks/internal/grpcserver"
	"finance-dashboard-provider-stocks/internal/subscribers"
	"finance-dashboard-provider-stocks/internal/update"

	tinkoff "github.com/TinkoffCreditSystems/invest-openapi-go-sdk"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
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

	updates := make(chan update.Update, 10)
	subs := subscribers.New()

	go startBroadcast(subs, updates)

	log.Printf("started broadcasting to subscribers")

	token := os.Getenv("TOKEN")
	rest := tinkoff.NewSandboxRestClient(token).RestClient // We don't need features of sandbox rest client.
	stream, err := tinkoff.NewStreamingClient(log, token)
	if err != nil {
		log.Fatal(err)
	}

	tickers := strings.Split(os.Getenv("TICKERS"), ";")
	instrumentsByFIGI, instrumentsByTickers := collectInstrumentsInfo(rest, tickers)

	c, err := populateCache(rest, instrumentsByFIGI)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("populated cache")

	cacheSubscriber := subs.Subscribe()
	defer subs.Unsubscribe(cacheSubscriber)

	go func() {
		for u := range cacheSubscriber.Updates {
			c.Set(u)
		}
	}()

	go processEventsFromAPI(stream, instrumentsByFIGI, updates)

	log.Printf("started processing events from API")

	for figi := range instrumentsByFIGI {
		if err := stream.SubscribeOrderbook(figi, 1, ""); err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("subscribed to orderbooks %v", tickers)

	go func() {
		for {
			time.Sleep(time.Second * 15)
			log.Printf("goroutines: %d, subscribers: %d", runtime.NumGoroutine(), subs.Len())
		}
	}()

	go listenAndServeGRPC(rest, instrumentsByTickers)
	listenAndServeHTTP(upgrader, subs, c)
}

func listenAndServeGRPC(rest *tinkoff.RestClient, instruments InstrumentMap) {
	lis, err := net.Listen("tcp", ":8082")
	if err != nil {
		log.Fatal(err)
	}

	server := grpc.NewServer()
	server.RegisterService(&api.CurrencyProvider_ServiceDesc, grpcserver.New(rest, instruments))

	log.Printf("grpc: listening on port 8082")
	log.Fatal(server.Serve(lis))
}

func listenAndServeHTTP(upgrader websocket.Upgrader, subs *subscribers.Subscribers, c *cache.Cache) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/stocks", HandleIncomingReq(upgrader, subs, c))

	server := http.Server{Addr: ":8081", Handler: mux}

	log.Printf("http: listening on port 8081")
	log.Fatal(server.ListenAndServe())
}

func populateCache(rest *tinkoff.RestClient, instrumentsByFIGI InstrumentMap) (*cache.Cache, error) {
	c := cache.New()

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

		c.Set(update.Update{
			Time:   lastCandle.TS,
			Name:   instrument.Name,
			Ticker: instrument.Ticker,
			Cost: update.Cost{
				Low:      lastCandle.ClosePrice,
				High:     lastCandle.ClosePrice,
				Currency: instrument.Currency,
			},
		})
	}

	return c, nil
}

func processEventsFromAPI(stream *tinkoff.StreamingClient, instrumentsByFIGI InstrumentMap, updates chan update.Update) {
	for {
		if err := stream.RunReadLoop(func(event interface{}) error {
			switch event := event.(type) {
			case tinkoff.OrderBookEvent:
				instrument := instrumentsByFIGI[event.OrderBook.FIGI]

				if len(event.OrderBook.Bids) == 0 || len(event.OrderBook.Asks) == 0 {
					log.Printf("no bids/asks for ticker %s (is exchange closed for ticker?)", instrument.Ticker)
					return nil
				}

				select {
				case updates <- update.Update{
					Time:   event.Time,
					Name:   instrument.Name,
					Ticker: instrument.Ticker,
					Cost: update.Cost{
						Low:      event.OrderBook.Bids[0][0],
						High:     event.OrderBook.Asks[0][0],
						Currency: instrument.Currency,
					},
				}:
				default:
					log.Printf("skipped event from API: updates channel is full")
				}
			default:
				return fmt.Errorf("unknown event %v", event)
			}

			return nil
		}); err != nil {
			log.Printf("error running read loop: %v", err)
		}
	}
}

func HandleIncomingReq(upgrader websocket.Upgrader, subscribers *subscribers.Subscribers, cache *cache.Cache) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("client subscribed")
		defer log.Printf("client unsubscribed")

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print(err)
			return
		}

		defer func() {
			_ = conn.Close()
		}()

		for _, u := range cache.Snapshot() {
			if ok := sendUpdate(conn, u); !ok {
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
				case u := <-subscriber.Updates:
					if ok := sendUpdate(conn, u); !ok {
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

func sendUpdate(conn *websocket.Conn, update update.Update) bool {
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

func collectInstrumentsInfo(rest *tinkoff.RestClient, tickers []string) (byFIGI InstrumentMap, byTickers InstrumentMap) {
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

func startBroadcast(subscribers *subscribers.Subscribers, updates <-chan update.Update) {
	for {
		u := <-updates

		snapshot := subscribers.Snapshot()
		for _, item := range snapshot {
			select {
			case item.Updates <- u:
			default:
				log.Printf("skipped sending info to subscriber: channel is full")
			}
		}
	}
}
