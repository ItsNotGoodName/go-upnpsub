package upnpsub

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/url"
	"time"
)

type ControlPointInterface interface {
	// ServeHTTP handles UPnP events from HTTP notify requests.
	ServeHTTP(http.ResponseWriter, *http.Request)
	// URI returns the URI that the ControlPoint has to be mounted on.
	URI() string
	// Port returns the port that the ControlPoint has to listens on.
	Port() int
	// Subscribe to event publisher and returns a Subscription.
	// Subscription is canceled when the provided context is done.
	// ControlPoint must be started before calling this function.
	Subscribe(ctx context.Context, eventURL *url.URL) (SubscriptionInterface, error)
}

type SubscriptionInterface interface {
	// Events returns channel that receives events from the UPnP event publisher.
	Events() <-chan *Event
	// Renew queues an early renewal of the subscription.
	Renew()
	// Active returns true if the subscription is active.
	Active() bool
	// LastActive returns the time the subscription was last active.
	LastActive() time.Time
	// Done returns channel that signals when the subscription is done cleaning up.
	Done() <-chan struct{}
}

// Property is the notify request's property.
type Property struct {
	Name  string // Name of inner field from UPnP property.
	Value string // Value of inner field from UPnP property.
}

// Event represents a parsed notify request.
type Event struct {
	Properties []Property
	SEQ        int
	sid        string
}

// propertyVariableXML represents the inner information of the property tag in the notify request's xml.
type propertyVariableXML struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

// propertyXML represents property tag in the notify request's xml.
type propertyXML struct {
	Property propertyVariableXML `xml:",any"`
}

// eventXML represents a notify request's xml.
type eventXML struct {
	XMLName    xml.Name      `xml:"urn:schemas-upnp-org:event-1-0 propertyset"`
	Properties []propertyXML `xml:"urn:schemas-upnp-org:event-1-0 property"`
}
