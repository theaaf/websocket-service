package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/sys/unix"

	wss "github.aaf.cloud/platform/websocket-service"
	"github.aaf.cloud/platform/websocket-service/transport"
)

func preferredIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP, nil
}

func serveServiceRequests(ctx context.Context, service *wss.Service, listener net.Listener) error {
	router := mux.NewRouter()
	router.Handle("/sr", transport.HTTPService{
		Service: service,
	}).Methods("POST")

	s := &http.Server{
		Handler:     router,
		ReadTimeout: 2 * time.Minute,
	}

	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		if err := s.Shutdown(context.Background()); err != nil {
			logrus.Error(err)
		}
		close(done)
	}()

	if err := s.Serve(listener); err != http.ErrServerClosed {
		return err
	}
	<-done
	return ctx.Err()
}

func serveWebSockets(ctx context.Context, service *wss.Service, port int) error {
	cors := handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods([]string{"GET", "HEAD", "POST", "OPTIONS"}),
	)

	listener, err := net.Listen("tcp4", fmt.Sprintf(":%v", port))
	if err != nil {
		return err
	}

	service.Logger.Infof("handling websocket connections at http://%v", listener.Addr())

	s := &http.Server{
		Handler:     cors(service),
		ReadTimeout: 2 * time.Minute,
	}

	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		if err := s.Shutdown(context.Background()); err != nil {
			logrus.Error(err)
		}
		close(done)
	}()

	if err := s.Serve(listener); err != http.ErrServerClosed {
		return err
	}
	<-done
	return ctx.Err()
}

func serve(ctx context.Context, args []string) error {
	flags := pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	originURL := flags.String("origin-url", "", "the origin's url")
	websocketPort := flags.Int("websocket-port", 0, "the port to use for websocket connections")
	serviceRequestPort := flags.Int("service-request-port", 0, "the port to use for service requests")
	keepAliveInterval := flags.Duration("keep-alive-interval", 0, "if given, the origin will receive keep-alive messages for websocket connections")
	subprotocols := flags.StringSlice("subprotocols", nil, "the subprotocols to negotiate on websocket connections")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *originURL == "" {
		return fmt.Errorf("an origin is required")
	}

	ipAddress, err := preferredIP()
	if err != nil {
		return err
	}

	serviceRequestListener, err := net.Listen("tcp4", fmt.Sprintf(":%v", *serviceRequestPort))
	if err != nil {
		return err
	}

	listenURI := fmt.Sprintf("%s:%d/sr", ipAddress.String(), serviceRequestListener.Addr().(*net.TCPAddr).Port)

	service := &wss.Service{
		Origin: &transport.HTTPOrigin{
			URL: *originURL,
		},
		Cluster: &transport.HTTPCluster{
			ListenURI: listenURI,
		},
		Logger:            logrus.StandardLogger(),
		KeepAliveInterval: *keepAliveInterval,
		Subprotocols:      *subprotocols,
	}
	defer service.Close()

	service.Logger.Infof("handling service requests at http://%v", listenURI)

	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(ctx)

	wg.Add(1)
	go func() {
		defer cancel()
		defer wg.Done()
		if err := serveWebSockets(ctx, service, *websocketPort); err != nil && err != context.Canceled {
			service.Logger.Error(err)
		}
	}()

	wg.Add(1)
	go func() {
		defer cancel()
		defer wg.Done()
		if err := serveServiceRequests(ctx, service, serviceRequestListener); err != nil && err != context.Canceled {
			service.Logger.Error(err)
		}
	}()

	wg.Wait()
	return nil
}

func main() {
	if !terminal.IsTerminal(unix.Stdout) {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		<-ch
		logrus.Info("signal caught. shutting down...")
		cancel()
	}()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := serve(ctx, os.Args[1:]); err != nil {
			logrus.Error(err)
		}
	}()

	wg.Wait()
}
