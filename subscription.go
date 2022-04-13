package upnpsub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Subscription struct {
	// Static fields.
	callbackHeader string        // callbackHeader is part of the UPnP header.
	doneC          chan struct{} // doneC is closed when the subscription is closed.
	eventC         chan *Event   // eventC is the events from UPnP event publisher.
	eventURL       string        // eventURL is the event URL of the UPnP event publisher.
	renewC         chan struct{} // renewC forces a subscription renewal.

	sid     string // sid the header set by the UPnP publisher.
	timeout int    // timeout is the timeout seconds received from UPnP publisher.

	activeTimeMu sync.Mutex // activeTimeMu protects activeTime.
	activeTime   time.Time  // activeTime is the time the subscription was last active.
}

func newSubscription(eventURL *url.URL, uri string, port int) (*Subscription, error) {
	callbackIP, err := findCallbackIP(eventURL)
	if err != nil {
		return nil, err
	}

	return &Subscription{
		callbackHeader: fmt.Sprintf("<http://%s:%d%s>", callbackIP, port, uri),
		eventURL:       eventURL.String(),
		doneC:          make(chan struct{}),
		eventC:         make(chan *Event, 8),
		renewC:         make(chan struct{}),
	}, nil
}

func (sub *Subscription) Renew() {
	select {
	case sub.renewC <- struct{}{}:
	default:
	}
}

func (sub *Subscription) Events() <-chan *Event {
	return sub.eventC
}

func (sub *Subscription) Done() <-chan struct{} {
	return sub.doneC
}

func (sub *Subscription) Active() bool {
	sub.activeTimeMu.Lock()
	active := time.Since(sub.activeTime) < time.Duration(sub.timeout)*time.Second
	sub.activeTimeMu.Unlock()
	return active
}

func (sub *Subscription) LastActive() time.Time {
	sub.activeTimeMu.Lock()
	t := sub.activeTime
	sub.activeTimeMu.Unlock()
	return t
}

func (sub *Subscription) setActive() {
	sub.activeTimeMu.Lock()
	sub.activeTime = time.Now()
	sub.activeTimeMu.Unlock()
}

func (sub *Subscription) setInactive() {
	sub.activeTimeMu.Lock()
	sub.activeTime = time.Unix(0, 0)
	sub.activeTimeMu.Unlock()
}

// subscribe sends SUBSCRIBE request to event publisher.
func (sub *Subscription) subscribe(ctx context.Context, sidHook func(oldSID, newSID string)) error {
	// Create request
	req, err := http.NewRequest("SUBSCRIBE", sub.eventURL, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)

	// Add headers to request
	req.Header.Add("CALLBACK", sub.callbackHeader)
	req.Header.Add("NT", headerNT)
	req.Header.Add("TIMEOUT", headerTimeout)

	// Execute request
	res, err := http.DefaultClient.Do(req)
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

	// Update sub's sid and call sid hook
	oldSID := sub.sid
	sub.sid = sid
	sidHook(oldSID, sub.sid)

	// Update sub's timeout
	timeout, err := parseTimeout(res.Header.Get("timeout"))
	if err != nil {
		return err
	}
	sub.timeout = timeout

	sub.setActive()

	return nil
}

// unsubscribe sends an UNSUBSCRIBE request to event publisher.
func (sub *Subscription) unsubscribe(ctx context.Context) error {
	// Create request
	req, err := http.NewRequest("UNSUBSCRIBE", sub.eventURL, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)

	// Add headers to request
	req.Header.Add("SID", sub.sid)

	// Execute request
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Check if request failed
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unsubscribe: invalid response status %s", res.Status)
	}

	sub.setInactive()

	return nil
}

// resubscribe sends a SUBSCRIBE request to event publisher that renews the existing subscription.
func (sub *Subscription) resubscribe(ctx context.Context) error {
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
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Check if request failed
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("resubscribe: invalid response status %s", res.Status)
	}

	// Check response's SID
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

	sub.setActive()

	return nil
}
