// Copyright © 2017 The Things Network
// Use of this source code is governed by the MIT license that can be found in the LICENSE file.

package monitor

import (
	"context"
	"time"

	"github.com/TheThingsNetwork/ttn/api/broker"
	"github.com/TheThingsNetwork/ttn/utils/backoff"
	"github.com/TheThingsNetwork/ttn/utils/errors"
	"github.com/golang/protobuf/ptypes/empty"
)

func (cl *brokerClient) initDownlink() {
	cl.downlink.ch = make(chan *broker.DownlinkMessage, BufferSize)
	go cl.monitorDownlink()
}

func (cl *brokerClient) monitorDownlink() {
	var retries int
newStream:
	for {
		ctx, cancel := context.WithCancel(cl.Context())
		cl.downlink.Lock()
		cl.downlink.cancel = cancel
		cl.downlink.Unlock()

		stream, err := cl.client.client.BrokerDownlink(ctx)
		if err != nil {
			cl.Ctx.WithError(errors.FromGRPCError(err)).Warn("Failed to open new monitor downlink stream")

			retries++
			time.Sleep(backoff.Backoff(retries))

			continue
		}
		retries = 0
		cl.Ctx.Debug("Opened new monitor downlink stream")

		// The actual stream
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case downlink, ok := <-cl.downlink.ch:
					if ok {
						stream.Send(downlink)
						cl.Ctx.Debug("Sent downlink to monitor")
					}
				}
			}
		}()

		msg := new(empty.Empty)
		for {
			if err := stream.RecvMsg(&msg); err != nil {
				cl.Ctx.WithError(errors.FromGRPCError(err)).Warn("Received error on monitor downlink stream, closing...")
				stream.CloseSend()
				cl.Ctx.Debug("Closed monitor downlink stream")

				cl.downlink.Lock()
				cl.downlink.cancel()
				cl.downlink.cancel = nil
				cl.downlink.Unlock()

				retries++
				time.Sleep(backoff.Backoff(retries))

				continue newStream
			}
		}
	}
}

func (cl *brokerClient) closeDownlink() {
	cl.downlink.Lock()
	defer cl.downlink.Unlock()
	if cl.downlink.cancel != nil {
		cl.downlink.cancel()
	}
}

// SendDownlink sends downlink to the monitor
func (cl *brokerClient) SendDownlink(downlink *broker.DownlinkMessage) (err error) {
	cl.downlink.init.Do(cl.initDownlink)

	select {
	case cl.downlink.ch <- downlink:
	default:
		cl.Ctx.Warn("Not sending downlink to monitor, buffer full")
		return errors.New("Not sending downlink to monitor, buffer full")
	}
	return
}
