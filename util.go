package upnpsub

import (
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultPort     = 8058              // DefaultPort is the port that the HTTP server listens on.
	DefaultURI      = "/eventSub"       // DefaultURI is the default URI where the UPnP publisher sends notify requests.
	defaultDeadline = 20 * time.Second  // defaultDeadline is the deadline for contexts.
	defaultTimeout  = 300               // defaultTimeout is the timeout in seconds for UPnP subscription.
	headerNT        = "upnp:event"      // headerNT is part of the UPnP header.
	headerNTS       = "upnp:propchange" // headerNTS is part of the UPnP header.
	headerTimeout   = "Second-300"      // headerTimeout is part of the UPnP header.
)

var timeoutReg = regexp.MustCompile(`(?i)second-([0-9]*)`)

// ListenAndServe is a wrapper for http.ListenAndServe.
func ListenAndServe(host string, cp ControlPoint) error {
	return http.ListenAndServe(host+":"+strconv.Itoa(cp.Port()), cp)
}

// parseTimeout parses the timeout from the UPnP header. Returns defaultTimeout if infinite.
func parseTimeout(timeout string) (int, error) {
	timeoutArray := timeoutReg.FindStringSubmatch(timeout)
	if len(timeoutArray) != 2 {
		return 0, errors.New("timeout not found")
	}

	timeoutString := timeoutArray[1]
	if strings.ToLower(timeoutString) == "infinite" {
		return defaultTimeout, nil
	}

	timeoutInt, err := strconv.Atoi(timeoutString)
	if err != nil {
		return 0, err
	}

	if timeoutInt < 0 {
		return 0, errors.New("timeout is invalid: " + timeoutString)
	}

	return timeoutInt, nil
}

// halfTimeoutDuration returns half the timeout as a time duration.
func halfTimeoutDuration(timeout int) time.Duration {
	return time.Duration(timeout/2) * time.Second
}

// parseEventXML parses eventXML from io.Reader.
func parseEventXML(r io.Reader) (*eventXML, error) {
	xmlEvent := &eventXML{}
	return xmlEvent, xml.NewDecoder(r).Decode(xmlEvent)
}

// parseProperties parses the properties from eventXML.
func parseProperties(xmlEvent *eventXML) []Property {
	properties := make([]Property, len(xmlEvent.Properties))
	for i := range xmlEvent.Properties {
		properties[i].Name = xmlEvent.Properties[i].Property.XMLName.Local
		properties[i].Value = xmlEvent.Properties[i].Property.Value
	}

	return properties
}

// findCallbackIP gets IP address of current machine that is reachable to the given url. https://stackoverflow.com/a/37382208
func findCallbackIP(url *url.URL) (string, error) {
	conn, err := net.Dial("udp", url.Host)
	if err != nil {
		return "", err
	}

	conn.Close()

	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}
