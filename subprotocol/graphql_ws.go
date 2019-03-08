package subprotocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	wss "github.com/theaaf/websocket-service"
)

type GraphQLWSCluster interface {
	Address() wss.Address

	SendGraphQLWSServiceRequest(address wss.Address, r *GraphQLWSServiceRequest) error
}

type GraphQLWSOrigin interface {
	SendGraphQLWSOriginRequest(*GraphQLWSOriginRequest) (*GraphQLWSOriginResponse, error)
}

type GraphQLWSOriginFunc func(*GraphQLWSOriginRequest) (*GraphQLWSOriginResponse, error)

func (f GraphQLWSOriginFunc) SendGraphQLWSServiceRequest(r *GraphQLWSOriginRequest) (*GraphQLWSOriginResponse, error) {
	return f(r)
}

// GraphQLWS implements the graphql-ws subprotocol. It can be used as a middle layer between the
// websocket service and an origin to provide a more efficient, higher level interface to the
// origin.
type GraphQLWS struct {
	Origin            GraphQLWSOrigin
	Cluster           GraphQLWSCluster
	KeepAliveInterval time.Duration
	Logger            logrus.FieldLogger

	initOnce sync.Once
	service  *wss.Service

	subscriptions                    map[string]*graphQLWSSubscription
	subscriptionsByKeys              map[string]map[*graphQLWSSubscription]struct{}
	subscriptionInputs               map[string]*graphQLWSSubscriptionInput
	subscriptionKeepAliveRequestTime time.Time
	mutex                            sync.Mutex
}

func (sp *GraphQLWS) Close() error {
	sp.init()
	return sp.service.Close()
}

type GraphQLWSServiceRequest struct {
	WebSocketServiceRequest *wss.ServiceRequest

	PublishRequest *GraphQLWSPublishRequest
}

type GraphQLWSPublishRequest struct {
	Subscribers []wss.Address
	Keys        []string
	Context     json.RawMessage
}

type GraphQLWSOriginRequest struct {
	Sender wss.Address

	StartRequest *GraphQLWSStartRequest

	SubscriptionQueryRequest *GraphQLWSSubscriptionQueryRequest

	SubscriptionKeepAliveRequest *GraphQLWSSubscriptionKeepAliveRequest
}

type GraphQLWSSubscriptionKeepAliveRequest struct {
	Keys []string
}

type GraphQLWSSubscriptionQueryRequest struct {
	Query         string
	Variables     json.RawMessage
	OperationName string
	Context       json.RawMessage
	InputContext  json.RawMessage
}

type GraphQLWSStartRequest struct {
	Query         string
	Variables     json.RawMessage
	OperationName string
}

type GraphQLWSOriginResponse struct {
	Error        string
	Result       *GraphQLWSResult
	Subscription *GraphQLWSSubscription
}

type GraphQLWSResult struct {
	Data   json.RawMessage
	Errors json.RawMessage
}

type GraphQLWSSubscription struct {
	// A digest that uniquely identifies the inputs to the query. This is used to batch query
	// execution.  Generally, this should be a hash of the normalized query, variables, and
	// operation name.
	InputDigest string

	InputContext json.RawMessage

	// A list of keys that will be used to trigger this subscription's execution in the future.
	Keys []string
}

type graphQLWSMessageType string

const (
	graphQLWSMessageTypeConnectionInit      graphQLWSMessageType = "connection_init"
	graphQLWSMessageTypeConnectionKeepAlive graphQLWSMessageType = "ka"
	graphQLWSMessageTypeConnectionTerminate graphQLWSMessageType = "connection_terminate"
	graphQLWSMessageTypeConnectionAck       graphQLWSMessageType = "connection_ack"
	graphQLWSMessageTypeComplete            graphQLWSMessageType = "complete"
	graphQLWSMessageTypeData                graphQLWSMessageType = "data"
	graphQLWSMessageTypeStart               graphQLWSMessageType = "start"
	graphQLWSMessageTypeStop                graphQLWSMessageType = "stop"
	graphQLWSMessageTypeError               graphQLWSMessageType = "error"
)

type graphQLWSMessage struct {
	Id      string               `json:"id,omitempty"`
	Type    graphQLWSMessageType `json:"type"`
	Payload json.RawMessage      `json:"payload,omitempty"`
}

type graphQLWSStartPayload struct {
	Query         string          `json:"query"`
	Variables     json.RawMessage `json:"variables"`
	OperationName string          `json:"operationName"`
}

type graphQLWSDataPayload struct {
	Data   json.RawMessage `json:"data,omitempty"`
	Errors json.RawMessage `json:"errors,omitempty"`
}

func (sp *GraphQLWS) init() {
	sp.initOnce.Do(func() {
		if sp.Logger == nil {
			sp.Logger = logrus.StandardLogger()
		}
		sp.service = &wss.Service{
			Cluster:           sp,
			Origin:            sp,
			Logger:            sp.Logger,
			Subprotocols:      []string{"graphql-ws"},
			KeepAliveInterval: 10 * time.Second,
		}
	})
}

func (sp *GraphQLWS) Address() wss.Address {
	return sp.Cluster.Address()
}

func (sp *GraphQLWS) SendServiceRequest(address wss.Address, r *wss.ServiceRequest) error {
	return sp.Cluster.SendGraphQLWSServiceRequest(address, &GraphQLWSServiceRequest{
		WebSocketServiceRequest: r,
	})
}

func (sp *GraphQLWS) HandleGraphQLWSServiceRequest(r *GraphQLWSServiceRequest) {
	sp.init()

	if r.WebSocketServiceRequest != nil {
		sp.service.HandleServiceRequest(r.WebSocketServiceRequest)
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	if r.PublishRequest != nil {
		for _, subscriber := range r.PublishRequest.Subscribers {
			subscriber := subscriber
			wg.Add(1)
			go func() {
				defer wg.Done()
				if subscriber.Equal(sp.Address()) {
					sp.handleLocalPublishRequest(r.PublishRequest)
				} else {
					copy := *r.PublishRequest
					copy.Subscribers = []wss.Address{subscriber}
					if err := sp.Cluster.SendGraphQLWSServiceRequest(subscriber, &GraphQLWSServiceRequest{
						PublishRequest: &copy,
					}); err != nil {
						sp.Logger.Error(errors.Wrap(err, "error forwarding publish request"))
					}
				}
			}()
		}
	}
}

func (sp *GraphQLWS) handleLocalPublishRequest(r *GraphQLWSPublishRequest) {
	subs := sp.getSubscriptionsByKeys(r.Keys)

	inputs := make(map[*graphQLWSSubscriptionInput][]*graphQLWSSubscription)
	for sub := range subs {
		inputs[sub.Input] = append(inputs[sub.Input], sub)
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	for input, subs := range inputs {
		input := input
		subs := subs
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := sp.Origin.SendGraphQLWSOriginRequest(&GraphQLWSOriginRequest{
				Sender: sp.Address(),
				SubscriptionQueryRequest: &GraphQLWSSubscriptionQueryRequest{
					Query:         input.Query,
					Variables:     input.Variables,
					OperationName: input.OperationName,
					Context:       r.Context,
					InputContext:  input.Context,
				},
			})
			if err != nil {
				sp.Logger.Error(errors.Wrap(err, "error sending subscription query request"))
				return
			}
			if resp.Result != nil {
				for _, sub := range subs {
					msg := sp.graphQLWSMessageWithGraphQLResult(sub.StartMessageId, resp.Result)
					if err := sp.sendGraphQLWSMessages([]wss.ConnectionId{sub.ConnectionId}, msg); err != nil {
						sp.Logger.Error(errors.Wrap(err, "error sending subscription data"))
					}
				}
			}
		}()
	}
}

func (sp *GraphQLWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sp.init()
	sp.service.ServeHTTP(w, r)
}

func (sp *GraphQLWS) sendGraphQLWSMessages(connectionIds []wss.ConnectionId, messages ...*graphQLWSMessage) error {
	serviceRequest := &wss.ServiceRequest{
		OutgoingWebSocketMessages: make([]*wss.OutgoingWebSocketMessage, len(messages)),
	}
	for i, message := range messages {
		encoded, err := jsoniter.Marshal(message)
		if err != nil {
			return err
		}
		text := string(encoded)
		serviceRequest.OutgoingWebSocketMessages[i] = &wss.OutgoingWebSocketMessage{
			ConnectionIds: connectionIds,
			Message: &wss.WebSocketMessage{
				Text: &text,
			},
		}
	}
	sp.service.HandleServiceRequest(serviceRequest)
	return nil
}

func (sp *GraphQLWS) SendOriginRequest(r *wss.OriginRequest) error {
	sp.handleOriginRequest(r)
	return nil
}

func (sp *GraphQLWS) graphQLWSMessageWithGraphQLResult(id string, result *GraphQLWSResult) *graphQLWSMessage {
	payload := &graphQLWSDataPayload{
		Data:   result.Data,
		Errors: result.Errors,
	}
	b, err := jsoniter.Marshal(payload)
	if err != nil {
		sp.Logger.Error(errors.Wrap(err, "error marshaling graphql-ws result payload"))
		return &graphQLWSMessage{
			Id:      id,
			Type:    graphQLWSMessageTypeError,
			Payload: json.RawMessage(`[{"message": "An internal error has occurred."}]`),
		}
	}
	return &graphQLWSMessage{
		Id:      id,
		Type:    graphQLWSMessageTypeData,
		Payload: b,
	}
}

func (sp *GraphQLWS) sendSubscriptionKeepAliveRequest() {
	if sp.KeepAliveInterval == 0 {
		return
	}

	var keys []string

	sp.mutex.Lock()
	if now := time.Now(); now.Sub(sp.subscriptionKeepAliveRequestTime) > sp.KeepAliveInterval {
		keys = make([]string, len(sp.subscriptionsByKeys))
		i := 0
		for key := range sp.subscriptionsByKeys {
			keys[i] = key
			i++
		}
		sp.subscriptionKeepAliveRequestTime = now
	}
	sp.mutex.Unlock()

	if len(keys) > 0 {
		if _, err := sp.Origin.SendGraphQLWSOriginRequest(&GraphQLWSOriginRequest{
			Sender: sp.Address(),
			SubscriptionKeepAliveRequest: &GraphQLWSSubscriptionKeepAliveRequest{
				Keys: keys,
			},
		}); err != nil {
			sp.Logger.Error(errors.Wrap(err, "error making graphql-ws subscription keep-alive request"))
		}
	}
}

func (sp *GraphQLWS) handleOriginRequest(r *wss.OriginRequest) {
	if len(r.WebSocketKeepAlives) > 0 {
		sp.sendSubscriptionKeepAliveRequest()

		sp.Logger.WithField("count", len(r.WebSocketKeepAlives)).Info("sending graphql-ws keep-alives")
		connectionIds := make([]wss.ConnectionId, len(r.WebSocketKeepAlives))
		for i, keepAlive := range r.WebSocketKeepAlives {
			connectionIds[i] = keepAlive.ConnectionId
		}
		if err := sp.sendGraphQLWSMessages(
			connectionIds,
			&graphQLWSMessage{
				Type: graphQLWSMessageTypeConnectionKeepAlive,
			},
		); err != nil {
			sp.Logger.Error(errors.Wrap(err, "unable to send graphql-ws keep-alives"))
		}
	}

	event := r.WebSocketEvent
	if event == nil {
		return
	}

	logger := sp.Logger.WithField("ws_connection_id", event.ConnectionId)

	if event.ConnectionClosed != nil {
		sp.removeConnection(event.ConnectionId)
	}

	if event.MessageReceived == nil {
		return
	}

	data := event.MessageReceived.Binary
	if text := event.MessageReceived.Text; text != nil {
		data = []byte(*text)
	}

	var msg graphQLWSMessage
	if err := jsoniter.Unmarshal(data, &msg); err != nil {
		logger.WithField("error", err.Error()).Info("malformed graphql-ws message received")
		return
	}

	switch msg.Type {
	case graphQLWSMessageTypeConnectionInit:
		if err := sp.sendGraphQLWSMessages(
			[]wss.ConnectionId{event.ConnectionId},
			&graphQLWSMessage{
				Id:   msg.Id,
				Type: graphQLWSMessageTypeConnectionAck,
			},
			&graphQLWSMessage{
				Type: graphQLWSMessageTypeConnectionKeepAlive,
			},
		); err != nil {
			logger.Error(errors.Wrap(err, "unable to send graphql-ws init response"))
		}
	case graphQLWSMessageTypeStart:
		var payload graphQLWSStartPayload
		if err := jsoniter.Unmarshal(msg.Payload, &payload); err != nil {
			logger.WithField("error", err.Error()).Info("malformed graphql-ws message received")
			return
		}

		response, err := sp.Origin.SendGraphQLWSOriginRequest(&GraphQLWSOriginRequest{
			Sender: sp.Address(),
			StartRequest: &GraphQLWSStartRequest{
				Query:         payload.Query,
				Variables:     payload.Variables,
				OperationName: payload.OperationName,
			},
		})
		if err != nil {
			logger.Error(errors.Wrap(err, "error making graphql-ws origin request"))
			return
		}

		var outgoing []*graphQLWSMessage

		if response != nil {
			if response.Error != "" {
				payload, err := jsoniter.Marshal([]struct {
					Message string `json:"message"`
				}{
					{Message: response.Error},
				})
				if err != nil {
					payload = []byte(`[{"message": "An internal error has occurred."}]`)
				}
				outgoing = append(outgoing, &graphQLWSMessage{
					Id:      msg.Id,
					Type:    graphQLWSMessageTypeError,
					Payload: json.RawMessage(payload),
				})
			} else {
				if response.Result != nil {
					outgoing = append(outgoing, sp.graphQLWSMessageWithGraphQLResult(msg.Id, response.Result))
				}

				if response.Subscription != nil {
					sp.addSubscription(event.ConnectionId, msg.Id, &payload, response.Subscription)
				} else {
					outgoing = append(outgoing, &graphQLWSMessage{
						Id:   msg.Id,
						Type: graphQLWSMessageTypeComplete,
					})
				}
			}
		}

		if len(outgoing) > 0 {
			if err := sp.sendGraphQLWSMessages(
				[]wss.ConnectionId{event.ConnectionId},
				outgoing...,
			); err != nil {
				logger.Error(errors.Wrap(err, "unable to send graphql-ws start response"))
			}
		}
	case graphQLWSMessageTypeStop:
		sp.removeSubscription(event.ConnectionId, msg.Id)
		if err := sp.sendGraphQLWSMessages(
			[]wss.ConnectionId{event.ConnectionId},
			&graphQLWSMessage{
				Id:   msg.Id,
				Type: graphQLWSMessageTypeComplete,
			},
		); err != nil {
			logger.Error(errors.Wrap(err, "unable to send graphql-ws stop response"))
		}
	case graphQLWSMessageTypeConnectionTerminate:
		// TODO: signal to the service to terminate the connection?
	default:
		logger.Info("unknown graphql-ws message type received")
	}
}

type graphQLWSSubscriptionInput struct {
	Query          string
	Variables      json.RawMessage
	OperationName  string
	Digest         string
	Context        json.RawMessage
	ReferenceCount int
}

type graphQLWSSubscription struct {
	ConnectionId   wss.ConnectionId
	StartMessageId string

	Input *graphQLWSSubscriptionInput

	// The set of keys that trigger this subscription.
	Keys []string
}

func globalSubscriptionId(connectionId wss.ConnectionId, startMessageId string) string {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutVarint(buf[:], int64(len(connectionId)))
	return string(buf[:n]) + string(connectionId) + startMessageId
}

func (sp *GraphQLWS) addSubscription(connectionId wss.ConnectionId, messageId string, start *graphQLWSStartPayload, subscription *GraphQLWSSubscription) error {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	if sp.subscriptions == nil {
		sp.subscriptions = make(map[string]*graphQLWSSubscription)
		sp.subscriptionsByKeys = make(map[string]map[*graphQLWSSubscription]struct{})
		sp.subscriptionInputs = make(map[string]*graphQLWSSubscriptionInput)
	}

	sub := &graphQLWSSubscription{
		ConnectionId:   connectionId,
		StartMessageId: messageId,
		Keys:           subscription.Keys,
	}

	if input, ok := sp.subscriptionInputs[subscription.InputDigest]; ok {
		input.ReferenceCount++
		sub.Input = input
	} else {
		input := &graphQLWSSubscriptionInput{
			Query:          start.Query,
			Variables:      start.Variables,
			OperationName:  start.OperationName,
			Digest:         subscription.InputDigest,
			Context:        subscription.InputContext,
			ReferenceCount: 1,
		}
		sp.subscriptionInputs[subscription.InputDigest] = input
		sub.Input = input
	}

	sp.subscriptions[globalSubscriptionId(sub.ConnectionId, sub.StartMessageId)] = sub

	for _, key := range sub.Keys {
		forKeys, ok := sp.subscriptionsByKeys[key]
		if !ok {
			forKeys = make(map[*graphQLWSSubscription]struct{})
			sp.subscriptionsByKeys[key] = forKeys
		}
		forKeys[sub] = struct{}{}
	}

	return nil
}

func (sp *GraphQLWS) getSubscriptionsByKeys(keys []string) map[*graphQLWSSubscription]struct{} {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	subs := make(map[*graphQLWSSubscription]struct{})
	for _, key := range keys {
		if forKey, ok := sp.subscriptionsByKeys[key]; ok {
			for sub := range forKey {
				subs[sub] = struct{}{}
			}
		}
	}
	return subs
}

func (sp *GraphQLWS) removeConnection(connectionId wss.ConnectionId) {
	subscriptions := sp.getSubscriptionsByConnectionId(connectionId)
	for _, subscription := range subscriptions {
		sp.removeSubscription(connectionId, subscription.StartMessageId)
	}
}

func (sp *GraphQLWS) removeSubscription(connectionId wss.ConnectionId, startMessageId string) {
	id := globalSubscriptionId(connectionId, startMessageId)

	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	sub, ok := sp.subscriptions[id]
	if !ok {
		return
	}

	for _, key := range sub.Keys {
		if forKeys, ok := sp.subscriptionsByKeys[key]; ok {
			delete(forKeys, sub)
			if len(forKeys) == 0 {
				delete(sp.subscriptionsByKeys, key)
			}
		}
	}

	sub.Input.ReferenceCount--
	if sub.Input.ReferenceCount == 0 {
		delete(sp.subscriptionInputs, sub.Input.Digest)
	}
	delete(sp.subscriptions, id)
}

func (sp *GraphQLWS) getSubscriptionsByConnectionId(connectionId wss.ConnectionId) []*graphQLWSSubscription {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()
	var ret []*graphQLWSSubscription
	for _, subscription := range sp.subscriptions {
		if bytes.Equal([]byte(subscription.ConnectionId), []byte(connectionId)) {
			ret = append(ret, subscription)
		}
	}
	return ret
}
