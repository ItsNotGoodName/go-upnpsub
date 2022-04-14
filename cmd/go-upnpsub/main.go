package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"

	"github.com/ItsNotGoodName/go-upnpsub"
)

func main() {
	urlPtr := flag.String("url", "", "UPnP event url (e.g. http://192.168.1.23:8050/421fec64-9d4a-40e7-9ce9-058c474fc209/Radio/event)")
	jsonPtr := flag.Bool("json", false, "Print json output")

	flag.Parse()

	if *urlPtr == "" {
		flag.Usage()
		return
	}

	URL, err := url.Parse(*urlPtr)
	if err != nil {
		log.Fatalln("invalid url:", err)
	}

	cp := upnpsub.NewControlPoint()
	go upnpsub.ListenAndServe("", cp)

	sub, err := cp.Subscribe(interruptContext(), URL)
	if err != nil {
		log.Fatalln("could not create subscription:", err)
	}

	var print func(event *upnpsub.Event)
	if *jsonPtr {
		print = printJSON
	} else {
		print = printText
	}

	for {
		select {
		case <-sub.Done():
			return
		case event := <-sub.Events():
			print(event)
		}
	}
}

type EventJSON struct {
	SEQ   int    `json:"seq"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func printJSON(event *upnpsub.Event) {
	for _, e := range event.Properties {
		b, err := json.Marshal(EventJSON{
			SEQ:   event.SEQ,
			Name:  e.Name,
			Value: e.Value,
		})
		if err != nil {
			panic(err)
		}
		fmt.Println(string(b))
	}
}

func printText(event *upnpsub.Event) {
	fmt.Println("> SEQ", event.SEQ)
	for _, e := range event.Properties {
		fmt.Printf("%q = %q\n", e.Name, e.Value)
	}
	fmt.Println("")
}

// interruptContext will return a context that will be cancelled on os interrupt signal.
func interruptContext() context.Context {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		cancel()
	}()

	return ctx
}
