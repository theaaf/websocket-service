package transport

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"

	wss "github.aaf.cloud/platform/websocket-service"
	jsoniter "github.com/json-iterator/go"
)

type HTTPService struct {
	Service *wss.Service
}

func (h HTTPService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var serviceRequest *wss.ServiceRequest
	if err := jsoniter.NewDecoder(r.Body).Decode(&serviceRequest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Service.HandleServiceRequest(serviceRequest)
}

type HTTPCluster struct {
	// The address, port, and optional path where this node can receive requests. For example,
	// this might be something like "10.0.1.20:1234/service-requests".
	ListenURI string
}

func (c *HTTPCluster) Address() wss.Address {
	return []byte(c.ListenURI)
}

func (c *HTTPCluster) SendServiceRequest(addr wss.Address, r *wss.ServiceRequest) error {
	b, err := jsoniter.Marshal(r)
	if err != nil {
		return err
	}
	resp, err := http.Post(fmt.Sprintf("http://%s", []byte(addr)), "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cluster responded with unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

type HTTPOrigin struct {
	URL string
}

func (o *HTTPOrigin) SendOriginRequest(r *wss.OriginRequest) error {
	b, err := jsoniter.Marshal(r)
	if err != nil {
		return err
	}
	resp, err := http.Post(o.URL, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("origin responded with unexpected status code: %d", resp.StatusCode)
	}

	return nil
}
