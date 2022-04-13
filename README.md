# go-upnpsub

Go library that handles subscribing to UPnP events.

## CLI

Use the command line version to test UPnP events.

### Install

```
go install github.com/ItsNotGoodName/go-upnpsub/cmd/go-upnpsub@latest
```

### Run

```
go-upnpsub -url http://192.168.1.23:8050/421fec64-9d4a-40e7-9ce9-058c474fc209/Radio/event
```

## Library

```go
package main

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/ItsNotGoodName/go-upnpsub"
)

func main() {
	// Create control point
	cp := upnpsub.NewControlPoint()
	go upnpsub.ListenAndServe("", cp)

	// Parse event url
	url, err := url.Parse("http://192.168.1.23:8050/421fec64-9d4a-40e7-9ce9-058c474fc209/Radio/event")
	if err != nil {
		panic(err)
	}

	// Create context that ends in 5 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create subscription
	sub, err := cp.Subscribe(ctx, url)
	if err != nil {
		panic(err)
	}

	// Print events until the context is done
	for {
		select {
		case <-sub.Done():
			// Subscription's context was canceled and it has finished cleaning up
			return
		case event := <-sub.Events():
			fmt.Printf("%+v\n", event)
		}
	}
}
```
