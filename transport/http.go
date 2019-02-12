package transport

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/viki-org/dnscache"

	wss "github.com/theaaf/websocket-service"
	"github.com/theaaf/websocket-service/subprotocol"
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
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		// Let the peer know right away that we're working on the request.
		flusher.Flush()
	}
	h.Service.HandleServiceRequest(serviceRequest)
}

type HTTPGraphQLWSService struct {
	Subprotocol *subprotocol.GraphQLWS
}

func (h HTTPGraphQLWSService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var serviceRequest *subprotocol.GraphQLWSServiceRequest
	if err := jsoniter.NewDecoder(r.Body).Decode(&serviceRequest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		// Let the peer know right away that we're working on the request.
		flusher.Flush()
	}
	h.Subprotocol.HandleGraphQLWSServiceRequest(serviceRequest)
}

type HTTPCluster struct {
	// The address, port, and optional path where this node can receive requests. For example,
	// this might be something like "10.0.1.20:1234/service-requests".
	ListenURI string
}

func (c *HTTPCluster) Address() wss.Address {
	return []byte(c.ListenURI)
}

var serviceHTTPTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   500 * time.Millisecond,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	Proxy:                 http.ProxyFromEnvironment,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	ResponseHeaderTimeout: 500 * time.Millisecond,
}

var serviceHTTPClient = &http.Client{
	Transport: serviceHTTPTransport,
	Timeout:   15 * time.Second,
}

func (c *HTTPCluster) SendServiceRequest(addr wss.Address, r *wss.ServiceRequest) error {
	return c.sendServiceRequest(addr, r)
}

func (c *HTTPCluster) SendGraphQLWSServiceRequest(addr wss.Address, r *subprotocol.GraphQLWSServiceRequest) error {
	return c.sendServiceRequest(addr, r)
}

func (c *HTTPCluster) sendServiceRequest(addr wss.Address, r interface{}) error {
	b, err := jsoniter.Marshal(r)
	if err != nil {
		return err
	}
	resp, err := serviceHTTPClient.Post(fmt.Sprintf("http://%s", []byte(addr)), "application/json", bytes.NewReader(b))
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

var dnsResolver = dnscache.New(time.Minute)

var originHTTPTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		separator := strings.LastIndex(addr, ":")
		ip, _ := dnsResolver.FetchOneString(addr[:separator])
		return (&net.Dialer{
			Timeout: 1 * time.Second,
		}).DialContext(ctx, "tcp", ip+addr[separator:])
	},
	MaxIdleConns:        100,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
}

var originHTTPClient = &http.Client{
	Transport: originHTTPTransport,
	Timeout:   15 * time.Second,
}

func (o *HTTPOrigin) Post(body interface{}) (*http.Response, error) {
	b, err := jsoniter.Marshal(body)
	if err != nil {
		return nil, err
	}
	return originHTTPClient.Post(o.URL, "application/json", bytes.NewReader(b))
}

func (o *HTTPOrigin) SendOriginRequest(r *wss.OriginRequest) error {
	resp, err := o.Post(r)
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

func (o *HTTPOrigin) SendGraphQLWSOriginRequest(r *subprotocol.GraphQLWSOriginRequest) (*subprotocol.GraphQLWSOriginResponse, error) {
	resp, err := o.Post(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("origin responded with unexpected status code: %d", resp.StatusCode)
	}

	var ret *subprotocol.GraphQLWSOriginResponse
	if err := jsoniter.NewDecoder(resp.Body).Decode(&ret); err != nil {
		return nil, errors.Wrap(err, "error reading response body")
	}
	return ret, nil
}
