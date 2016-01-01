// Copyright © 2015 The Things Network
// Use of this source code is governed by the MIT license that can be found in the LICENSE file.

// package rtr_brk_http
//
// Assume one endpoint url accessible through a POST http request
package rtr_brk_http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/thethingsnetwork/core"
	"github.com/thethingsnetwork/core/lorawan/semtech"
	"github.com/thethingsnetwork/core/utils/log"
	"net/http"
	"sync"
)

type Adapter struct {
	Logger log.Logger
}

// Listen implements the core.Adapter interface
func (a *Adapter) Listen(router core.Router, options interface{}) error {
	return nil
}

// Broadcast implements the core.BrokerRouter interface
func (a *Adapter) Broadcast(router core.Router, payload semtech.Payload, broAddrs ...core.BrokerAddress) error {
	// Determine the devAddress associated to that payload
	if payload.RXPK == nil || len(payload.RXPK) == 0 { // NOTE are those conditions significantly different ?
		a.log("Cannot broadcast given payload: %+v", payload)
		return core.ErrInvalidPayload
	}

	devAddr, err := payload.UniformDevAddr()
	if err != nil {
		a.log("Cannot broadcast given payload: %+v", payload)
		return core.ErrInvalidPayload
	}

	// Prepare ground to store brokers that are in charge
	register := make(chan core.BrokerAddress, len(broAddrs))
	wg := sync.WaitGroup{}
	wg.Add(len(broAddrs))

	client := http.Client{}
	for _, addr := range broAddrs {
		go func(addr core.BrokerAddress) {
			defer wg.Done()

			resp, err := post(client, string(addr), payload)

			if err != nil {
				a.log("Unable to send POST request %+v", err)
				router.HandleError(core.ErrBroadcast) // NOTE Mote information should be sent
				return
			}

			defer resp.Body.Close()

			switch resp.StatusCode {
			case http.StatusOK:
				a.log("Broker %+v handles packets coming from %+v", addr, devAddr)
				register <- addr
			case http.StatusNotFound: //NOTE Convention with the broker
				a.log("Broker %+v does not handle packets coming from %+v", addr, devAddr)
			default:
				a.log("Unexpected answer from the broker %+v", err)
				router.HandleError(core.ErrBroadcast) // NOTE More information should be sent
			}
		}(addr)
	}

	go func() {
		wg.Wait()
		close(register)
		brokers := make([]core.BrokerAddress, 0)
		for addr := range register {
			brokers = append(brokers, addr)
		}
		if len(brokers) > 0 {
			router.RegisterDevice(*devAddr, brokers...)
		}
	}()

	return nil
}

// Forward implements the core.BrokerRouter interface
func (a *Adapter) Forward(router core.Router, payload semtech.Payload, broAddrs ...core.BrokerAddress) error {
	if payload.RXPK == nil || len(payload.RXPK) == 0 { // NOTE are those conditions significantly different ?
		a.log("Cannot broadcast given payload: %+v", payload)
		return core.ErrInvalidPayload
	}
	client := http.Client{}
	for _, addr := range broAddrs {
		go func(url string) {
			a.log("Send payload to %s", url)
			resp, err := post(client, url, payload)

			if err != nil {
				a.log("Unable to send POST request %+v", err)
				router.HandleError(core.ErrForward) // NOTE More information should be sent
				return
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				a.log("Unexpected answer from the broker %+v", err)
				router.HandleError(core.ErrForward) // NOTE More information should be sent
				return
			}

			// NOTE Do We Care about the response ? The router is supposed to handle HTTP request
			// from the broker to handle packets or anything else ? Is it efficient ? Should
			// downlinks packets be sent back with the HTTP body response ? Its a 2 seconds frame...

		}(string(addr))
	}

	return nil
}

// post regroups some logic used in both Forward and Broadcast methods
func post(client http.Client, url string, payload semtech.Payload) (*http.Response, error) {
	data := new(bytes.Buffer)
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	if _, err := data.Write(rawJSON); err != nil {
		return nil, err
	}

	return client.Post(url, "application/json", data)
}

// log is nothing more than a shortcut / helper to access the logger
func (a Adapter) log(format string, i ...interface{}) {
	if a.Logger == nil {
		return
	}
	a.Logger.Log(format, i...)
}
