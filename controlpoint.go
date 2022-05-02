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

type controlPoint struct {
	uri  string // uri is the URI that the ControlPoint has to be mounted on.
	port int    // port is the port that the ControlPoint has to listen on.

	sidMapRWMu sync.RWMutex             // sidMapRWMu protects sidMap.
	sidMap     map[string]*subscription // sidMap hold all active subscriptions.
}

// ControlPointOption is an option for ControlPoint.
type ControlPointOption func(*controlPoint)

// WithPort sets the port for ControlPoint.
func WithPort(port int) ControlPointOption {
	return func(cp *controlPoint) { cp.port = port }
}

// WithURI sets the uri for ControlPoint.
func WithURI(uri string) ControlPointOption {
	return func(cp *controlPoint) { cp.uri = uri }
}

// NewControlPoint creates a new ControlPoint.
func NewControlPoint(options ...ControlPointOption) ControlPoint {
	cp := &controlPoint{
		uri:        DefaultURI,
		port:       DefaultPort,
		sidMap:     make(map[string]*subscription),
		sidMapRWMu: sync.RWMutex{},
	}

	for _, option := range options {
		option(cp)
	}

	return cp
}

func (cp *controlPoint) URI() string {
	return cp.uri
}

func (cp *controlPoint) Port() int {
	return cp.port
}

func (cp *controlPoint) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	// Get SEQ
	var seq int
	if seqStr := r.Header.Get("SEQ"); seqStr != "" {
		seqInt, err := strconv.Atoi(seqStr)
		if err != nil {
			log.Println("controlPoint.ServeHTTP(WARNING): invalid seq:", err)
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		seq = seqInt
	}

	// Get NT and NTS
	nt, nts := r.Header.Get("NT"), r.Header.Get("NTS")
	if nt == "" || nts == "" {
		log.Println("controlPoint.ServeHTTP(WARNING): request has no nt or nts")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	// Validate NT and NTS
	if nt != headerNT || nts != headerNTS {
		log.Printf("controlPoint.ServeHTTP(WARNING): invalid nt or nts, %s, %s", nt, nts)
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
		log.Println("controlPoint.ServeHTTP(WARNING): sid not found or valid:", sid)
		rw.WriteHeader(http.StatusPreconditionFailed)
		return
	}

	// Parse xmlEvent from body
	xmlEvent, err := parseEventXML(r.Body)
	if err != nil {
		log.Println("controlPoint.ServeHTTP(WARNING):", err)
		return
	}

	// Parse properties from xmlEvent
	properties := parseProperties(xmlEvent)

	// Try to send event to subscription's event channel
	t := time.NewTimer(defaultDeadline)
	select {
	case <-t.C:
		log.Println("controlPoint.ServeHTTP(ERROR): could not send event to subscription's event channel")
	case sub.eventC <- Event{Properties: properties, SEQ: seq}:
		if !t.Stop() {
			<-t.C
		}
	}
}

func (cp *controlPoint) Subscribe(ctx context.Context, eventURL *url.URL) (Subscription, error) {
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

// subscriptionLoop handles sending subscribe requests to UPnP event publisher.
func (cp *controlPoint) subscriptionLoop(ctx context.Context, sub *subscription, d time.Duration) {
	log.Printf("controlPoint.subscriptionLoop: %s: started", sub.eventURL)

	t := time.NewTimer(d)
	renew := func() {
		d, err := cp.renew(ctx, sub)
		if err != nil {
			log.Println("controlPoint.subscriptionLoop(ERROR):", err)
		}
		t.Reset(d)
	}

	for {
		select {
		case <-ctx.Done():
			log.Printf("controlPoint.subscriptionLoop: %s: closing", sub.eventURL)

			// Delete sub.sid from sidMap
			cp.sidMapRWMu.Lock()
			delete(cp.sidMap, sub.sid)
			cp.sidMapRWMu.Unlock()

			// Unsubscribe
			ctx, cancel := context.WithTimeout(context.Background(), defaultDeadline)
			if err := sub.unsubscribe(ctx); err != nil {
				log.Println("controlPoint.subscriptionLoop(ERROR):", err)
			}
			cancel()

			log.Printf("controlPoint.subscriptionLoop: %s: closed", sub.eventURL)
			close(sub.doneC)
			return
		case <-sub.renewC:
			// Manual renew
			log.Printf("controlPoint.subscriptionLoop: %s: starting manual renewal", sub.eventURL)
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
func (cp *controlPoint) renew(ctx context.Context, sub *subscription) (time.Duration, error) {
	if !sub.IsActive() {
		if err := sub.subscribe(ctx, func(oldSID, newSID string) {
			cp.sidMapRWMu.Lock()
			delete(cp.sidMap, oldSID)
			cp.sidMap[newSID] = sub
			cp.sidMapRWMu.Unlock()
		}); err != nil {
			return defaultDeadline, err
		}

		d := halfTimeoutDuration(sub.timeout)
		log.Printf("controlPoint.renew: %s: subscribe successful, will resubscribe in %s intervals", sub.eventURL, d)

		return d, nil
	}

	if err := sub.resubscribe(ctx); err != nil {
		return defaultDeadline, err
	}

	return halfTimeoutDuration(sub.timeout), nil
}
