package upnpsub

import (
	"context"
	"log"
)

// Renew tells subscription to renew if it is not already renewing.
func (sub *Subscription) Renew() {
	select {
	case sub.renewChan <- true:
	default:
	}
}

// activeLoop handles active status of subscription.
func (sub *Subscription) activeLoop() {
	log.Println("Subscription.activeLoop: started")

	active := false
	for {
		select {
		case <-sub.Done:
			close(sub.Active)
			return
		case active = <-sub.setActiveChan:
		case sub.Active <- active:
		}
	}
}

// setActive sets active status of subscription.
func (sub *Subscription) setActive(ctx context.Context, active bool) {
	select {
	case <-ctx.Done():
	case sub.setActiveChan <- active:
	}
}
