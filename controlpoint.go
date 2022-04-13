// Package upnpsub handles subscribing to UPnP events.
// It tries to follow section 4 of "UPnP Device Architecture 1.0".
package upnpsub

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// ControlPointImpl handles the HTTP notify server and keeps track of subscriptions.
type ControlPointImpl struct {
	client     *http.Client // client is the http client to use for making subscribe, resubscribe, and unsubscribe http requests.
	listenURI  string       // listenURI is the URI for consuming notify requests.
	listenPort string       // listenPort is the HTTP server's port.

	sidMapRWMu sync.RWMutex                 // sidMapRWMu read-write mutex.
	sidMap     map[string]*SubscriptionImpl // sidMap hold all active subscriptions.
}

// SubscriptionImpl represents the subscription to the UPnP publisher.
type SubscriptionImpl struct {
	ActiveC    chan bool   // ActiveC represents the subscription status to publisher.
	DoneC      chan bool   // DoneC is closed when subscriptionLoop is finished.
	EventC     chan *Event // EventC is the UPnP events from publisher.
	callback   string      // callback is part of the UPnP header.
	eventURL   string      // eventURL is the URL we are subscribed on the event publisher.
	renewC     chan bool   // renewC forces a subscription renewal.
	setActiveC chan bool   // SetActiveC sets the active status.
	sid        string      // sid the header set by the UPnP publisher.
	timeout    int         // timeout is the timeout seconds received from UPnP publisher.
}

// NewControlPoint creates a ControlPoint.
func NewControlPoint() *ControlPointImpl {
	return NewControlPointWithPort(DefaultPort)
}

// NewControlPoint creates a ControlPoint that listens on a specific port.
func NewControlPointWithPort(listenPort int) *ControlPointImpl {
	cp := &ControlPointImpl{
		client:     &http.Client{},
		listenURI:  DefaultURI,
		listenPort: fmt.Sprint(listenPort),
		sidMap:     make(map[string]*SubscriptionImpl),
		sidMapRWMu: sync.RWMutex{},
	}

	http.Handle(cp.listenURI, cp)
	return cp
}

// Start HTTP server that listens for notify requests.
func (cp *ControlPointImpl) Start() {
	log.Println("ControlPoint.Start: listening on port", cp.listenPort)
	log.Fatal(http.ListenAndServe(":"+cp.listenPort, nil))
}

// NewSubscription subscribes to event publisher and returns a Subscription.
// Errors if the event publisher is unreachable or if the initial SUBSCRIBE request fails.
func (cp *ControlPointImpl) NewSubscription(ctx context.Context, eventURL *url.URL) (*SubscriptionImpl, error) {
	// Find callback ip
	callbackIP, err := findCallbackIP(eventURL)
	if err != nil {
		return nil, err
	}

	// Create sub
	sub := &SubscriptionImpl{
		ActiveC:    make(chan bool),
		DoneC:      make(chan bool),
		EventC:     make(chan *Event),
		callback:   fmt.Sprintf("<http://%s:%s%s>", callbackIP, cp.listenPort, cp.listenURI),
		eventURL:   eventURL.String(),
		renewC:     make(chan bool),
		setActiveC: make(chan bool),
	}

	go sub.activeLoop()

	// Subscribe
	d, err := cp.renew(ctx, sub)
	if err != nil {
		close(sub.DoneC)
		return nil, err
	}

	go cp.subscriptionLoop(ctx, sub, d)

	return sub, nil
}

// ServeHTTP handles notify requests from event publishers.
func (cp *ControlPointImpl) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	// Get NT and NTS
	nt, nts := r.Header.Get("NT"), r.Header.Get("NTS")
	if nt == "" || nts == "" {
		log.Println("ControlPoint.ServeHTTP(WARNING): request has no nt or nts")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get SEQ
	var seq int
	if seqStr := r.Header.Get("SEQ"); seqStr != "" {
		seqInt, err := strconv.Atoi(seqStr)
		if err != nil {
			log.Println("ControlPoint.ServeHTTP(WARNING): invalid seq", err)
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		seq = seqInt
	}

	// Validate NT and NTS
	if nt != headerNT || nts != headerNTS {
		log.Printf("ControlPoint.ServeHTTP(WARNING): invalid nt or nts, %s, %s", nt, nts)
		rw.WriteHeader(http.StatusPreconditionFailed)
		return
	}

	// Get SID
	sid := r.Header.Get("SID")

	// Find sub from sidMap using SID
	cp.sidMapRWMu.RLock()
	sub, ok := cp.sidMap[sid]
	cp.sidMapRWMu.RUnlock()
	if !ok {
		log.Println("ControlPoint.ServeHTTP(WARNING): sid not found or valid,", sid)
		rw.WriteHeader(http.StatusPreconditionFailed)
		return
	}

	// Parse xmlEvent from body
	xmlEvent, err := parseEventXML(r.Body)
	if err != nil {
		log.Println("ControlPoint.ServeHTTP(WARNING):", err)
		return
	}

	// Parse properties from xmlEvent
	properties := parseProperties(xmlEvent)

	// Try to send event to sub's EventC
	t := time.NewTimer(defaultDeadline)
	select {
	case <-t.C:
		log.Println("ControlPoint.ServeHTTP(ERROR): could not send event to subscription's EventC")
	case sub.EventC <- &Event{Properties: properties, SEQ: seq, sid: sid}:
		if !t.Stop() {
			<-t.C
		}
	}
}

// subscriptionLoop handles sending subscribe requests to event publisher.
func (cp *ControlPointImpl) subscriptionLoop(ctx context.Context, sub *SubscriptionImpl, d time.Duration) {
	log.Println("ControlPoint.subscriptionLoop: started")

	t := time.NewTimer(d)
	renew := func() {
		d, err := cp.renew(ctx, sub)
		if err != nil {
			log.Print("ControlPoint.subscriptionLoop(ERROR):", err)
		}
		t.Reset(d)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("ControlPoint.subscriptionLoop: closing")

			// Delete sub.sid from sidMap
			cp.sidMapRWMu.Lock()
			delete(cp.sidMap, sub.sid)
			cp.sidMapRWMu.Unlock()

			// Unsubscribe
			ctx, cancel := context.WithTimeout(context.Background(), defaultDeadline)
			if err := cp.unsubscribe(ctx, sub); err != nil {
				log.Print("ControlPoint.subscriptionLoop(ERROR):", err)
			}
			cancel()

			close(sub.DoneC)

			log.Println("ControlPoint.subscriptionLoop: closed")
			return
		case <-sub.renewC:
			// Manual renew
			log.Println("ControlPoint.subscriptionLoop: starting manual renewal")
			if !t.Stop() {
				<-t.C
			}
			renew()
		case <-t.C:
			// Renew
			renew()
		}
	}
}

// renew handles subscribing or resubscribing.
func (cp *ControlPointImpl) renew(ctx context.Context, sub *SubscriptionImpl) (time.Duration, error) {
	if !<-sub.ActiveC {
		if err := cp.subscribe(ctx, sub); err != nil {
			return halfTimeoutDuration(sub.timeout), err
		}
		sub.setActive(ctx, true)
		d := halfTimeoutDuration(sub.timeout)
		log.Printf("ControlPoint.renew: subscribe successful, will resubscribe in %s intervals", d)
		return d, nil
	}
	if err := cp.resubscribe(ctx, sub); err != nil {
		sub.setActive(ctx, false)
		return defaultDeadline, err
	}
	return halfTimeoutDuration(sub.timeout), nil
}

// subscribe sends SUBSCRIBE request to event publisher.
func (cp *ControlPointImpl) subscribe(ctx context.Context, sub *SubscriptionImpl) error {
	// Create request
	req, err := http.NewRequest("SUBSCRIBE", sub.eventURL, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)

	// Add headers to request
	req.Header.Add("CALLBACK", sub.callback)
	req.Header.Add("NT", headerNT)
	req.Header.Add("TIMEOUT", headerTimeout)

	// Execute request
	res, err := cp.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Check if request failed
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("subscribe: invalid response status %s", res.Status)
	}

	// Get SID
	sid := res.Header.Get("sid")
	if sid == "" {
		return errors.New("subscribe: response did not supply a sid")
	}

	cp.sidMapRWMu.Lock()

	// Delete old SID to sub mapping
	delete(cp.sidMap, sub.sid)

	// Add new SID to sub mapping
	sub.sid = sid
	cp.sidMap[sid] = sub

	cp.sidMapRWMu.Unlock()

	// Update sub's timeout
	timeout, err := parseTimeout(res.Header.Get("timeout"))
	if err != nil {
		return err
	}
	sub.timeout = timeout

	return nil
}

// unsubscribe sends an UNSUBSCRIBE request to event publisher.
func (cp *ControlPointImpl) unsubscribe(ctx context.Context, sub *SubscriptionImpl) error {
	// Create request
	req, err := http.NewRequest("UNSUBSCRIBE", sub.eventURL, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)

	// Add headers to request
	req.Header.Add("SID", sub.sid)

	// Execute request
	res, err := cp.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Check if request failed
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unsubscribe: invalid response status %s", res.Status)
	}

	return nil
}

// resubscribe sends a SUBSCRIBE request to event publisher that renews the existing subscription.
func (cp *ControlPointImpl) resubscribe(ctx context.Context, sub *SubscriptionImpl) error {
	// Create request
	req, err := http.NewRequest("SUBSCRIBE", sub.eventURL, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)

	// Add headers to request
	req.Header.Add("SID", sub.sid)
	req.Header.Add("TIMEOUT", headerTimeout)

	// Execute request
	res, err := cp.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Check if request failed
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("resubscribe: invalid response status %s", res.Status)
	}

	// Check request's SID header
	sid := res.Header.Get("SID")
	if sid == "" {
		return errors.New("resubscribe: response did not supply a sid")
	}
	if sid != sub.sid {
		return fmt.Errorf("resubscribe: response's sid does not match subscription's sid, %s != %s", sid, sub.sid)
	}

	// Update sub's timeout
	timeout, err := parseTimeout(res.Header.Get("timeout"))
	if err != nil {
		return err
	}
	sub.timeout = timeout

	return nil
}
