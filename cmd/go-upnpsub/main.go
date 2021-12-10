package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"

	"github.com/ItsNotGoodName/go-upnpsub"
)

func main() {
	// Parse user input
	urlPtr := flag.String("url", "", "UPnP event url (e.g. http://192.168.1.23:8050/421fec64-9d4a-40e7-9ce9-058c474fc209/Radio/event)")

	flag.Parse()

	if *urlPtr == "" {
		flag.Usage()
		return
	}

	URL, err := url.Parse(*urlPtr)
	if err != nil {
		fmt.Println("Invalid url:", err)
		return
	}

	// Create controlpoint
	cp := upnpsub.NewControlPoint()
	go cp.Start()

	// Create subscription
	sub, err := cp.NewSubscription(signalContext(), URL)
	if err != nil {
		fmt.Println("Could not create subscription:", err)
		return
	}

	// Print events until done
	for {
		select {
		case <-sub.Done:
			return
		case event := <-sub.Event:
			fmt.Println(">>>>> SEQ", event.SEQ)
			for _, e := range event.Properties {
				fmt.Println(e.Name, "=", e.Value)
			}
		}
	}
}

// signalContext will return a context that will be cancelled on interrupt signal.
func signalContext() context.Context {
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
