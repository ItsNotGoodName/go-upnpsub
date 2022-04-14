package upnpsub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/ItsNotGoodName/go-upnpsub/internal/state"
)

type subscription struct {
	// Static fields.
	callbackHeader string        // callbackHeader is part of the UPnP header.
	doneC          chan struct{} // doneC is closed when the subscription is closed.
	eventC         chan *Event   // eventC is the events from UPnP event publisher.
	eventURL       string        // eventURL is the event URL of the UPnP event publisher.
	renewC         chan struct{} // renewC forces a subscription renewal.
	state          *state.State  // state is the state of the subscription.

	sid     string // sid the header set by the UPnP publisher.
	timeout int    // timeout is the timeout seconds received from UPnP publisher.

}

func newSubscription(eventURL *url.URL, uri string, port int) (*subscription, error) {
	callbackIP, err := findCallbackIP(eventURL)
	if err != nil {
		return nil, err
	}

	return &subscription{
		callbackHeader: fmt.Sprintf("<http://%s:%d%s>", callbackIP, port, uri),
		doneC:          make(chan struct{}),
		eventC:         make(chan *Event, 8),
		eventURL:       eventURL.String(),
		renewC:         make(chan struct{}),
		timeout:        minTimeout,
		state:          state.New(),
	}, nil
}

func (sub *subscription) Renew() {
	select {
	case sub.renewC <- struct{}{}:
	default:
	}
}

func (sub *subscription) Events() <-chan *Event {
	return sub.eventC
}

func (sub *subscription) Done() <-chan struct{} {
	return sub.doneC
}

func (sub *subscription) IsActive() bool {
	select {
	case <-sub.doneC:
		return false
	default:
		return sub.state.Active()
	}
}

func (sub *subscription) LastActive() time.Time {
	return sub.state.LastActive()
}

// subscribe sends SUBSCRIBE request to event publisher.
func (sub *subscription) subscribe(ctx context.Context, sidHook func(oldSID, newSID string)) error {
	success := false
	defer func() {
		if success {
			sub.state.Activate(sub.timeout)
		} else {
			sub.state.Deactivate()
		}
	}()

	// Create request
	req, err := http.NewRequest("SUBSCRIBE", sub.eventURL, nil)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	req = req.WithContext(ctx)

	// Add headers to request
	req.Header.Add("CALLBACK", sub.callbackHeader)
	req.Header.Add("NT", headerNT)
	req.Header.Add("TIMEOUT", headerTimeout)

	// Execute request
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer res.Body.Close()

	// Check if request failed
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("subscribe: invalid response status %s", res.Status)
	}

	// Get sub's timeout
	timeout, err := parseTimeout(res.Header.Get("timeout"))
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	// Get SID
	sid := res.Header.Get("sid")
	if sid == "" {
		return errors.New("subscribe: response did not supply a sid")
	}

	sub.timeout = timeout
	oldSID := sub.sid
	sub.sid = sid
	sidHook(oldSID, sub.sid)
	success = true

	return nil
}

// resubscribe sends a SUBSCRIBE request to event publisher that renews the existing subscription.
func (sub *subscription) resubscribe(ctx context.Context) error {
	success := false
	defer func() {
		if success {
			sub.state.Activate(sub.timeout)
		} else {
			sub.state.Deactivate()
		}
	}()

	// Create request
	req, err := http.NewRequest("SUBSCRIBE", sub.eventURL, nil)
	if err != nil {
		return fmt.Errorf("resubscribe: %w", err)
	}
	req = req.WithContext(ctx)

	// Add headers to request
	req.Header.Add("SID", sub.sid)
	req.Header.Add("TIMEOUT", headerTimeout)

	// Execute request
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("resubscribe: %w", err)
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

	// Get sub's timeout
	timeout, err := parseTimeout(res.Header.Get("timeout"))
	if err != nil {
		return fmt.Errorf("resubscribe: %w", err)
	}

	sub.timeout = timeout
	success = true

	return nil
}

// unsubscribe sends an UNSUBSCRIBE request to event publisher.
func (sub *subscription) unsubscribe(ctx context.Context) error {
	// Create request
	req, err := http.NewRequest("UNSUBSCRIBE", sub.eventURL, nil)
	if err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	req = req.WithContext(ctx)

	// Add headers to request
	req.Header.Add("SID", sub.sid)

	// Execute request
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	defer res.Body.Close()

	// Check if request failed
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unsubscribe: invalid response status %s", res.Status)
	}

	sub.state.Deactivate()

	return nil
}
