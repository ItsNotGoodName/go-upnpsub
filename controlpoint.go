// Package upnpsub handles subscribing to UPnP events.
// It tries to follow section 4 of "UPnP Device Architecture 1.0".
package upnpsub

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// ControlPoint handles the HTTP notify server and keeps track of subscriptions.
type ControlPoint struct {
	uri  string // uri is the URI for consuming notify requests.
	port int    // listenport is the HTTP server's port.

	sidMapRWMu sync.RWMutex             // sidMapRWMu read-write mutex.
	sidMap     map[string]*Subscription // sidMap hold all active subscriptions.
}

func OptionPort(port int) func(cp *ControlPoint) {
	return func(cp *ControlPoint) { cp.port = port }
}

func OptionURI(uri string) func(cp *ControlPoint) {
	return func(cp *ControlPoint) { cp.uri = uri }
}

// NewControlPoint creates a ControlPoint with port and URI.
func NewControlPoint(opts ...func(cp *ControlPoint)) ControlPointInterface {
	cp := &ControlPoint{
		uri:        DefaultURI,
		port:       DefaultPort,
		sidMap:     make(map[string]*Subscription),
		sidMapRWMu: sync.RWMutex{},
	}

	for _, opt := range opts {
		opt(cp)
	}

	return cp
}

func (cp *ControlPoint) URI() string {
	return cp.uri
}

func (cp *ControlPoint) Port() int {
	return cp.port
}

func (cp *ControlPoint) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	// Get SEQ
	var seq int
	if seqStr := r.Header.Get("SEQ"); seqStr != "" {
		seqInt, err := strconv.Atoi(seqStr)
		if err != nil {
			log.Println("ControlPoint.ServeHTTP(WARNING): invalid seq:", err)
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		seq = seqInt
	}

	// Get NT and NTS
	nt, nts := r.Header.Get("NT"), r.Header.Get("NTS")
	if nt == "" || nts == "" {
		log.Println("ControlPoint.ServeHTTP(WARNING): request has no nt or nts")
		rw.WriteHeader(http.StatusBadRequest)
		return
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

	// Try to send event to subscription's event channel
	t := time.NewTimer(defaultDeadline)
	select {
	case <-t.C:
		log.Println("ControlPoint.ServeHTTP(ERROR): could not send event to subscription's event channel")
	case sub.eventC <- &Event{Properties: properties, SEQ: seq, sid: sid}:
		if !t.Stop() {
			<-t.C
		}
	}
}

func (cp *ControlPoint) Subscribe(ctx context.Context, eventURL *url.URL) (SubscriptionInterface, error) {
	// Create sub
	sub, err := newSubscription(eventURL, cp.uri, cp.port)
	if err != nil {
		return nil, err
	}

	// Initial subscribe to check if it's possible to subscribe to the eventURL
	d, err := cp.renew(ctx, sub)
	if err != nil {
		return nil, err
	}

	// Start subscription loop
	go cp.subscriptionLoop(ctx, sub, d)

	return sub, nil
}

// subscriptionLoop handles sending subscribe requests to event publisher.
func (cp *ControlPoint) subscriptionLoop(ctx context.Context, sub *Subscription, d time.Duration) {
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
			if err := sub.unsubscribe(ctx); err != nil {
				log.Print("ControlPoint.subscriptionLoop(ERROR):", err)
			}
			cancel()

			close(sub.doneC)

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
func (cp *ControlPoint) renew(ctx context.Context, sub *Subscription) (time.Duration, error) {
	if !sub.Active() {
		if err := sub.subscribe(ctx, func(oldSID, newSID string) {
			cp.sidMapRWMu.Lock()
			delete(cp.sidMap, oldSID)
			cp.sidMap[newSID] = sub
			cp.sidMapRWMu.Unlock()
		}); err != nil {
			return halfTimeoutDuration(sub.timeout), err
		}

		d := halfTimeoutDuration(sub.timeout)
		log.Printf("ControlPoint.renew: subscribe successful, will resubscribe in %s intervals", d)

		return d, nil
	}

	if err := sub.resubscribe(ctx); err != nil {
		return defaultDeadline, err
	}

	return halfTimeoutDuration(sub.timeout), nil
}
